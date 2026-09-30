package main

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/raghavs6/KVFlow/internal/workerpb"
	"github.com/raghavs6/KVFlow/internal/xfer"
)

// listen returns a localhost listener that closes when the test ends.
func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln
}

// startClient serves newServer on a localhost port and returns a real gRPC
// client for it, so calls go through gRPC's encoding and transport.
func startClient(t *testing.T) workerpb.WorkerClient {
	t.Helper()
	s := newServer()
	ln := listen(t)
	go s.Serve(ln)
	t.Cleanup(s.Stop)
	cc, err := grpc.NewClient(ln.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	t.Cleanup(func() { cc.Close() })
	return workerpb.NewWorkerClient(cc)
}

// A Transfer sent over gRPC reaches the worker, and the worker's measured
// time comes back in the reply.
func TestTransferOverGRPC(t *testing.T) {
	data := listen(t)
	go xfer.ServeAll(data, t.Logf)
	client := startClient(t)

	reply, err := client.Transfer(context.Background(), &workerpb.TransferRequest{PeerAddr: data.Addr().String(), Bytes: 3 << 20})
	if err != nil {
		t.Fatalf("Transfer() error = %v", err)
	}
	if reply.GetSeconds() <= 0 {
		t.Errorf("Transfer() seconds = %v, want > 0", reply.GetSeconds())
	}
}

// The client's deadline travels with the call and stops the worker's send,
// rather than only ending the client's wait while the worker keeps going.
func TestClientDeadlineReachesWorker(t *testing.T) {
	stuck := listen(t)
	held := make(chan net.Conn, 1)
	go func() {
		// Accept and never read, so the worker's send blocks.
		if c, err := stuck.Accept(); err == nil {
			held <- c
		}
	}()
	t.Cleanup(func() {
		select {
		case c := <-held:
			c.Close()
		default:
		}
	})
	client := startClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := client.Transfer(ctx, &workerpb.TransferRequest{PeerAddr: stuck.Addr().String(), Bytes: 64 << 20})
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("Transfer() error = %v, want code DeadlineExceeded", err)
	}

	// The worker gave up too: if its send were still blocked it would hold
	// the lock, and this transfer to a healthy peer would wait behind it.
	data := listen(t)
	go xfer.ServeAll(data, t.Logf)
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := client.Transfer(ctx, &workerpb.TransferRequest{PeerAddr: data.Addr().String(), Bytes: 10}); err != nil {
		t.Errorf("Transfer() after the deadline error = %v, want nil", err)
	}
}
