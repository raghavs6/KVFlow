package worker

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

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
