// Package worker carries out the controller's commands on a KVFlow worker.
package worker

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

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
func (w *Worker) Transfer(ctx context.Context, req *workerpb.TransferRequest) (*workerpb.TransferReply, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	conn, err := w.conn(req.GetPeerAddr())
	if err != nil {
		return nil, err
	}
	start := time.Now()
	if err := xfer.Send(conn, req.GetBytes()); err != nil {
		// conn may have stopped partway through a transfer, and the
		// receiver would misread whatever came next on it. Drop it so
		// the next transfer dials fresh. There is no retry: the
		// controller decides what a failed transfer means.
		conn.Close()
		delete(w.conns, req.GetPeerAddr())
		return nil, err
	}
	return &workerpb.TransferReply{Seconds: time.Since(start).Seconds()}, nil
}

// conn returns the open connection to addr, dialing it the first time.
// w.mu must be held.
func (w *Worker) conn(addr string) (net.Conn, error) {
	if c, ok := w.conns[addr]; ok {
		return c, nil
	}
	c, err := net.Dial("tcp", addr)
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
