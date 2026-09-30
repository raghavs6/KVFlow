// Package worker carries out the controller's commands on a KVFlow worker.
package worker

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/raghavs6/KVFlow/internal/workerpb"
	"github.com/raghavs6/KVFlow/internal/xfer"
)

// Worker sends KV bytes to peers over one reused TCP connection per peer,
// because the transfer model only holds for warm, reused connections.
type Worker struct {
	workerpb.UnimplementedWorkerServer

	// mu is held for a whole transfer, so two transfers never mix their
	// bytes on a shared connection.
	mu    sync.Mutex
	conns map[string]net.Conn
}

// New returns a Worker with no open connections.
func New() *Worker {
	return &Worker{conns: make(map[string]net.Conn)}
}

// Transfer sends req's bytes to its peer and replies with how long the send
// took. Only the send is timed: waiting for another transfer to finish and
// dialing a new connection are not network time the learner should see.
//
// Errors carry gRPC codes so the controller can tell its own bugs
// (InvalidArgument) from network trouble (Unavailable) and from giving up
// itself (DeadlineExceeded, Canceled).
func (w *Worker) Transfer(ctx context.Context, req *workerpb.TransferRequest) (*workerpb.TransferReply, error) {
	// Checked before the lock so a bad request never waits in line. The
	// learner divides by bytes, so a transfer must have some.
	if req.GetBytes() <= 0 || req.GetPeerAddr() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "want bytes > 0 and a peer address, got %d bytes to %q", req.GetBytes(), req.GetPeerAddr())
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	// Waiting for a mutex can't be interrupted, so a request whose caller
	// gave up while it waited stops here instead of starting a transfer.
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}

	addr := req.GetPeerAddr()
	conn, err := w.conn(ctx, addr)
	if err != nil {
		return nil, failure(ctx, "dial peer", err)
	}
	// A TCP write doesn't watch ctx, so when ctx ends, expire conn's
	// deadline to wake a send that is blocked on a peer that stopped
	// reading.
	stop := context.AfterFunc(ctx, func() { conn.SetDeadline(time.Now()) })
	start := time.Now()
	err = xfer.Send(conn, req.GetBytes())
	elapsed := time.Since(start)
	// stop is false if the tripwire already fired, possibly just after a
	// successful send, leaving conn's deadline in the past for the next
	// transfer. That window is too narrow to test reliably.
	if !stop() || err != nil {
		// After a failed send, conn may have stopped partway through a
		// transfer, and the receiver would misread whatever came next on
		// it. Drop it so the next transfer dials fresh. There is no
		// retry: the controller decides what a failed transfer means.
		conn.Close()
		delete(w.conns, addr)
	}
	if err != nil {
		return nil, failure(ctx, "send to peer", err)
	}
	return &workerpb.TransferReply{Seconds: elapsed.Seconds()}, nil
}

// failure turns a dial or send error into a gRPC error. If ctx has ended,
// the caller gave up and the error only reports that, so the code says so
// rather than blaming the network.
func failure(ctx context.Context, what string, err error) error {
	if ctx.Err() != nil {
		return status.FromContextError(ctx.Err()).Err()
	}
	return status.Errorf(codes.Unavailable, "%s: %v", what, err)
}

// conn returns the open connection to addr, dialing it the first time.
// w.mu must be held.
func (w *Worker) conn(ctx context.Context, addr string) (net.Conn, error) {
	if c, ok := w.conns[addr]; ok {
		return c, nil
	}
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	w.conns[addr] = c
	return c, nil
}

// Close closes every open connection.
func (w *Worker) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	var errs []error
	for addr, c := range w.conns {
		errs = append(errs, c.Close())
		delete(w.conns, addr)
	}
	return errors.Join(errs...)
}
