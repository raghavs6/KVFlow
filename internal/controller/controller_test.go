package controller

import (
	"context"
	"net"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/raghavs6/KVFlow/internal/learner"
	"github.com/raghavs6/KVFlow/internal/scheduler"
	"github.com/raghavs6/KVFlow/internal/simulator"
	"github.com/raghavs6/KVFlow/internal/worker"
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

// startCluster runs an xfer receiver and a gRPC worker allowed to send to it,
// both on localhost until the test ends, and returns a client for the worker.
func startCluster(t *testing.T) (workerpb.WorkerClient, *countingListener) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	recv := &countingListener{Listener: ln}
	t.Cleanup(func() { ln.Close() })
	go xfer.ServeAll(recv, t.Logf)

	grpcLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	w := worker.New([]string{recv.Addr().String()})
	server := grpc.NewServer()
	workerpb.RegisterWorkerServer(server, w)
	go server.Serve(grpcLn)
	t.Cleanup(func() { server.Stop(); w.Close() })

	conn, err := grpc.NewClient(grpcLn.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return workerpb.NewWorkerClient(conn), recv
}

// scenarios returns n requests for a 1 MiB prefix where waiting never wins,
// recomputing takes about a second, and transferring wins at any bandwidth
// above about 1 MB/s.
func scenarios(n int) []simulator.Scenario {
	return simulator.Stable(n, simulator.Scenario{
		Source:          simulator.Worker{Queue: 10 * time.Second, PrefillTokensPerSec: 1000},
		Destination:     simulator.Worker{PrefillTokensPerSec: 1000},
		Request:         simulator.Request{PrefixTokens: 1024, SuffixTokens: 10},
		KVBytesPerToken: 1024,
	})
}

// A learner that believes the network is fast transfers every time, over one
// reused connection, and learns from what the worker measured.
func TestRunLearnsFromRealTransfers(t *testing.T) {
	client, recv := startCluster(t)
	const initial = 1e-9 // 1 GB/s
	l, err := learner.NewEWMA(0.5, initial)
	if err != nil {
		t.Fatal(err)
	}

	actions, err := Run(context.Background(), client, recv.Addr().String(), l, scenarios(5))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := slices.Repeat([]scheduler.Action{scheduler.ActionTransfer}, 5)
	if !slices.Equal(actions, want) {
		t.Errorf("actions = %v, want %v", actions, want)
	}
	if got := recv.accepted.Load(); got != 1 {
		t.Errorf("receiver accepted %d connections, want 1", got)
	}
	if got := l.SecondsPerByte(); got == initial || got <= 0 {
		t.Errorf("SecondsPerByte() = %v, want positive and changed from %v", got, initial)
	}
	t.Logf("learned %.2f GB/s", 1/l.SecondsPerByte()/1e9)
}

// A learner that believes the network is slow always recomputes, so it never
// measures the network and never finds out it was wrong.
func TestRunNeverLearnsWithoutTransfers(t *testing.T) {
	client, recv := startCluster(t)
	const initial = 1e-3 // 1 KB/s
	l, err := learner.NewEWMA(0.5, initial)
	if err != nil {
		t.Fatal(err)
	}

	actions, err := Run(context.Background(), client, recv.Addr().String(), l, scenarios(5))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := slices.Repeat([]scheduler.Action{scheduler.ActionRecompute}, 5)
	if !slices.Equal(actions, want) {
		t.Errorf("actions = %v, want %v", actions, want)
	}
	if got := recv.accepted.Load(); got != 0 {
		t.Errorf("receiver accepted %d connections, want 0", got)
	}
	if got := l.SecondsPerByte(); got != initial {
		t.Errorf("SecondsPerByte() = %v, want unchanged %v", got, initial)
	}
}
