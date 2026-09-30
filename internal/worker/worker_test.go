package worker

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"slices"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/raghavs6/KVFlow/internal/workerpb"
	"github.com/raghavs6/KVFlow/internal/xfer"
)

// countingListener counts accepted connections.
type countingListener struct {
	net.Listener
	accepted atomic.Int32
}

func (l *countingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.accepted.Add(1)
	}
	return c, err
}

// startReceiver runs an xfer receiver on a localhost port until the test ends.
func startReceiver(t *testing.T) *countingListener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	counted := &countingListener{Listener: ln}
	t.Cleanup(func() { ln.Close() })
	go xfer.ServeAll(counted, t.Logf)
	return counted
}

// newWorker returns a Worker whose connections close when the test ends.
func newWorker(t *testing.T) *Worker {
	t.Helper()
	w := New()
	t.Cleanup(func() { w.Close() })
	return w
}

// Transfers to one peer report a positive time and share one connection.
func TestTransferReusesConnection(t *testing.T) {
	recv := startReceiver(t)
	w := newWorker(t)
	for _, n := range []int64{10, 3 << 20} {
		reply, err := w.Transfer(context.Background(), &workerpb.TransferRequest{
			PeerAddr: recv.Addr().String(),
			Bytes:    n,
		})
		if err != nil {
			t.Fatalf("Transfer(%d) error = %v", n, err)
		}
		if reply.GetSeconds() <= 0 {
			t.Errorf("Transfer(%d) seconds = %v, want > 0", n, reply.GetSeconds())
		}
	}
	if got := recv.accepted.Load(); got != 1 {
		t.Errorf("receiver accepted %d connections, want 1", got)
	}
}

// A transfer that fails partway leaves its connection mid-transfer, so the
// worker must drop it and dial a fresh one for the next transfer.
func TestFailedTransferDiscardsConnection(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	counted := &countingListener{Listener: ln}
	go func() {
		// The first connection dies after the header, mid-transfer.
		conn, err := counted.Accept()
		if err != nil {
			return
		}
		var header [8]byte
		io.ReadFull(conn, header[:])
		conn.Close()
		xfer.ServeAll(counted, t.Logf)
	}()

	w := newWorker(t)
	req := &workerpb.TransferRequest{PeerAddr: ln.Addr().String(), Bytes: 3 << 20}
	if _, err := w.Transfer(context.Background(), req); status.Code(err) != codes.Unavailable {
		t.Fatalf("first Transfer() error = %v, want code Unavailable from the dropped connection", err)
	}
	if _, err := w.Transfer(context.Background(), req); err != nil {
		t.Errorf("second Transfer() error = %v, want nil on a fresh connection", err)
	}
	if got := counted.accepted.Load(); got != 2 {
		t.Errorf("receiver accepted %d connections, want 2", got)
	}
}

// A peer nobody listens on is reported as Unavailable, not Unknown, so the
// controller can tell a network problem from a bug.
func TestDeadPeerIsUnavailable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // nothing listens here now, so dials are refused

	w := newWorker(t)
	_, err = w.Transfer(context.Background(), &workerpb.TransferRequest{PeerAddr: addr, Bytes: 10})
	if status.Code(err) != codes.Unavailable {
		t.Errorf("Transfer() error = %v, want code Unavailable", err)
	}
}

// Requests that can't be a transfer are rejected before any dial. Zero
// bytes matters most: the learner divides by bytes.
func TestInvalidRequestIsRejected(t *testing.T) {
	recv := startReceiver(t)
	w := newWorker(t)
	for _, req := range []*workerpb.TransferRequest{
		{PeerAddr: recv.Addr().String(), Bytes: 0},
		{PeerAddr: recv.Addr().String(), Bytes: -1},
		{PeerAddr: "", Bytes: 10},
	} {
		if _, err := w.Transfer(context.Background(), req); status.Code(err) != codes.InvalidArgument {
			t.Errorf("Transfer(%v) error = %v, want code InvalidArgument", req, err)
		}
	}
	if got := recv.accepted.Load(); got != 0 {
		t.Errorf("receiver accepted %d connections, want 0", got)
	}
}

// startRecorder accepts one connection and reads transfers in xfer's format
// like xfer.Serve, but also records each size it read. Once the sender
// closes, it sends the sizes on the returned channel.
func startRecorder(t *testing.T) (string, <-chan []int64) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	sizes := make(chan []int64, 1)
	go func() {
		var got []int64
		defer func() { sizes <- got }()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var header [8]byte
		for {
			if _, err := io.ReadFull(conn, header[:]); err != nil {
				return
			}
			n := int64(binary.BigEndian.Uint64(header[:]))
			got = append(got, n)
			if _, err := io.CopyN(io.Discard, conn, n); err != nil {
				return
			}
			if _, err := conn.Write([]byte{1}); err != nil {
				return
			}
		}
	}()
	return ln.Addr().String(), sizes
}

// Concurrent transfers to one peer take turns on the shared connection.
// Success alone can't show that: the bytes are zeros, so a header that lands
// inside another transfer's body is read as body, and later zeros are read
// as a 0-byte transfer whose reply unblocks some other sender. So the
// receiver's sizes must match the sizes sent.
func TestConcurrentTransfersDoNotMix(t *testing.T) {
	addr, received := startRecorder(t)
	w := New()
	var sent []int64
	errs := make(chan error, 8)
	for i := range 8 {
		// Not multiples of xfer's 1 MiB write chunk, so writes end
		// mid-chunk where another transfer's could slip in.
		n := int64(i+1)<<20 + 12345
		sent = append(sent, n)
		go func() {
			_, err := w.Transfer(context.Background(), &workerpb.TransferRequest{PeerAddr: addr, Bytes: n})
			errs <- err
		}()
	}
	for range 8 {
		if err := <-errs; err != nil {
			t.Errorf("Transfer() error = %v", err)
		}
	}
	w.Close() // ends the recorder
	got := <-received
	slices.Sort(got)
	if !slices.Equal(got, sent) {
		t.Errorf("receiver read sizes %v, want %v", got, sent)
	}
}
