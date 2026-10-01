package controller

import (
	"context"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

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
	return startWorker(t, recv.Addr().String()), recv
}

// startWorker runs a gRPC worker on localhost, allowed to send to peer,
// until the test ends, and returns a client for it.
func startWorker(t *testing.T, peer string) workerpb.WorkerClient {
	t.Helper()
	grpcLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	w := worker.New([]string{peer})
	server := grpc.NewServer()
	workerpb.RegisterWorkerServer(server, w)
	go server.Serve(grpcLn)
	t.Cleanup(func() { server.Stop(); w.Close() })

	conn, err := grpc.NewClient(grpcLn.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return workerpb.NewWorkerClient(conn)
}

// startStuckPeer accepts connections but never reads, so a large send
// blocks once the TCP buffers fill. Its connections close when the test
// ends.
func startStuckPeer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	var mu sync.Mutex
	var held []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range held {
			c.Close()
		}
	})
	return ln.Addr().String()
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

// actions returns the action taken for each result.
func actions(results []Result) []scheduler.Action {
	out := make([]scheduler.Action, len(results))
	for i, r := range results {
		out[i] = r.Action
	}
	return out
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

	results, err := Run(context.Background(), client, l, scenarios(5), Config{PeerAddr: recv.Addr().String(), Timeout: time.Second})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := slices.Repeat([]scheduler.Action{scheduler.ActionTransfer}, 5)
	if got := actions(results); !slices.Equal(got, want) {
		t.Errorf("actions = %v, want %v", got, want)
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

	results, err := Run(context.Background(), client, l, scenarios(5), Config{PeerAddr: recv.Addr().String(), Timeout: time.Second})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := slices.Repeat([]scheduler.Action{scheduler.ActionRecompute}, 5)
	if got := actions(results); !slices.Equal(got, want) {
		t.Errorf("actions = %v, want %v", got, want)
	}
	for i, r := range results {
		if r.Seconds != 0 {
			t.Errorf("results[%d].Seconds = %v, want 0 without a transfer", i, r.Seconds)
		}
	}
	if got := recv.accepted.Load(); got != 0 {
		t.Errorf("receiver accepted %d connections, want 0", got)
	}
	if got := l.SecondsPerByte(); got != initial {
		t.Errorf("SecondsPerByte() = %v, want unchanged %v", got, initial)
	}
}

// Probing forces a transfer every few requests, so a learner that wrongly
// believes the network is slow measures it and switches to transferring.
func TestProbingCorrectsSlowBelief(t *testing.T) {
	client, recv := startCluster(t)
	l, err := learner.NewEWMA(0.5, 1e-3) // 1 KB/s
	if err != nil {
		t.Fatal(err)
	}

	// Alpha 0.5 halves a 1000x-too-slow belief once per probe, so it takes
	// about 11 probes, one every 3 requests, before transfer wins on its own.
	const n = 60
	results, err := Run(context.Background(), client, l, scenarios(n), Config{PeerAddr: recv.Addr().String(), ProbeEvery: 2, Timeout: time.Second})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	actions := actions(results)
	first := slices.Index(actions, scheduler.ActionTransfer)
	if first != 2 {
		t.Errorf("first transfer at request %d, want 2 (the first probe)", first)
	}
	tail := slices.Repeat([]scheduler.Action{scheduler.ActionTransfer}, 10)
	if !slices.Equal(actions[n-10:], tail) {
		t.Errorf("last 10 actions = %v, want all transfers", actions[n-10:])
	}
	if got := recv.accepted.Load(); got != 1 {
		t.Errorf("receiver accepted %d connections, want 1", got)
	}
	probes := 0
	for i, r := range results {
		if r.Believed != r.Action {
			probes++
			if r.Believed != scheduler.ActionRecompute || r.Action != scheduler.ActionTransfer {
				t.Errorf("results[%d] believed %s but took %s, want only recompute forced to transfer", i, r.Believed, r.Action)
			}
		}
		if r.Action == scheduler.ActionTransfer && (r.PredictedSeconds <= 0 || r.Seconds <= 0) {
			t.Errorf("results[%d] = %+v, want positive predicted and measured seconds", i, r)
		}
	}
	// After 10 probes the belief sits almost exactly on the tipping point,
	// so the real link's speed decides whether one more is needed.
	if probes < 10 || probes > 12 {
		t.Errorf("%d probes, want about 11", probes)
	}
	t.Logf("first probe: %+v", results[first])
	t.Logf("last: %+v", results[n-1])
	t.Logf("learned %.2f GB/s", 1/l.SecondsPerByte()/1e9)
}

// When every transfer fails, the run still finishes: each failure is
// recorded, the learner learns nothing, and failed attempts stay spaced out
// by the probe counter.
func TestFailedTransfersAreRecorded(t *testing.T) {
	client, recv := startCluster(t)
	recv.Close() // the peer is gone, so the worker can't dial it
	const initial = 1e-3
	l, err := learner.NewEWMA(0.5, initial)
	if err != nil {
		t.Fatal(err)
	}

	results, err := Run(context.Background(), client, l, scenarios(9), Config{PeerAddr: recv.Addr().String(), ProbeEvery: 2, Timeout: time.Second})
	if err != nil {
		t.Fatalf("Run() error = %v, want failures recorded per request", err)
	}
	want := []scheduler.Action{
		scheduler.ActionRecompute, scheduler.ActionRecompute, scheduler.ActionTransfer,
		scheduler.ActionRecompute, scheduler.ActionRecompute, scheduler.ActionTransfer,
		scheduler.ActionRecompute, scheduler.ActionRecompute, scheduler.ActionTransfer,
	}
	if got := actions(results); !slices.Equal(got, want) {
		t.Errorf("actions = %v, want %v", got, want)
	}
	for i, r := range results {
		failed := r.Action == scheduler.ActionTransfer
		if (r.Err != nil) != failed || r.Seconds != 0 {
			t.Errorf("results[%d] = %+v, want Err set only on transfers and Seconds 0", i, r)
		}
	}
	if got := l.SecondsPerByte(); got != initial {
		t.Errorf("SecondsPerByte() = %v, want unchanged %v", got, initial)
	}
	t.Logf("failure: %v", results[2].Err)
}

// Ending ctx stops the run rather than being recorded as a failure.
func TestCanceledRunStops(t *testing.T) {
	client, recv := startCluster(t)
	l, err := learner.NewEWMA(0.5, 1e-9)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results, err := Run(ctx, client, l, scenarios(3), Config{PeerAddr: recv.Addr().String(), Timeout: time.Second})
	if err == nil {
		t.Error("Run() error = nil, want the cancellation")
	}
	if len(results) != 0 {
		t.Errorf("Run() returned %d results, want 0: nothing finished", len(results))
	}
}

func TestRunRejectsNegativeProbe(t *testing.T) {
	l, err := learner.NewEWMA(0.5, 1e-9)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), nil, l, scenarios(1), Config{ProbeEvery: -1, Timeout: time.Second}); err == nil {
		t.Error("Run(probeEvery = -1) error = nil, want an error")
	}
}

func TestRunRejectsNonPositiveTimeout(t *testing.T) {
	l, err := learner.NewEWMA(0.5, 1e-9)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), nil, l, scenarios(1), Config{}); err == nil {
		t.Error("Run(timeout = 0) error = nil, want an error")
	}
}

// A transfer to a peer that stopped reading fails at the timeout and is
// recorded, and the run goes on to the next request.
func TestStuckTransferTimesOut(t *testing.T) {
	peer := startStuckPeer(t)
	client := startWorker(t, peer)

	l, err := learner.NewEWMA(0.5, 1e-9)
	if err != nil {
		t.Fatal(err)
	}
	// 1 GiB is far more than the TCP buffers hold.
	big := scenarios(2)
	for i := range big {
		big[i].Request.PrefixTokens = 1 << 20
	}

	const timeout = 200 * time.Millisecond
	start := time.Now()
	results, err := Run(context.Background(), client, l, big, Config{PeerAddr: peer, Timeout: timeout})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run() error = %v, want timeouts recorded per request", err)
	}
	for i, r := range results {
		if status.Code(r.Err) != codes.DeadlineExceeded {
			t.Errorf("results[%d].Err = %v, want DeadlineExceeded", i, r.Err)
		}
	}
	if elapsed > 5*timeout {
		t.Errorf("Run() took %v for 2 transfers with a %v timeout", elapsed, timeout)
	}
	// The second request starts once the first has waited out its timeout.
	if gap := results[1].At - results[0].At; gap < timeout || gap > 5*timeout {
		t.Errorf("second request started %v after the first, want about %v", gap, timeout)
	}
	t.Logf("took %v; started at %v and %v; failure: %v", elapsed, results[0].At, results[1].At, results[0].Err)
}

func TestRunRejectsNegativeInterval(t *testing.T) {
	l, err := learner.NewEWMA(0.5, 1e-9)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), nil, l, scenarios(1), Config{Timeout: time.Second, Interval: -1}); err == nil {
		t.Error("Run(Interval = -1) error = nil, want an error")
	}
}

// Requests start on a fixed schedule, one every Interval, however long each
// transfer takes.
func TestIntervalPacesRequests(t *testing.T) {
	client, recv := startCluster(t)
	l, err := learner.NewEWMA(0.5, 1e-9)
	if err != nil {
		t.Fatal(err)
	}
	// 64 MiB takes ~20 ms on localhost, so pacing that waited Interval after
	// each request, instead of keeping a schedule, would fall behind it.
	slow := scenarios(5)
	for i := range slow {
		slow[i].Request.PrefixTokens = 64 << 10
	}
	const interval = 50 * time.Millisecond
	results, err := Run(context.Background(), client, l, slow, Config{
		PeerAddr: recv.Addr().String(),
		Timeout:  time.Second,
		Interval: interval,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for i, r := range results {
		slot := time.Duration(i) * interval
		if r.At < slot || r.At > slot+30*time.Millisecond {
			t.Errorf("results[%d].At = %v, want in [%v, %v]", i, r.At, slot, slot+30*time.Millisecond)
		}
		t.Logf("request %d started at %v, transfer took %.1f ms", i, r.At, r.Seconds*1e3)
	}
}

// Canceling ctx ends a run that is waiting for its next request's slot, and
// returns the requests that had finished.
func TestCancelStopsPacingWait(t *testing.T) {
	client, recv := startCluster(t)
	l, err := learner.NewEWMA(0.5, 1e-9)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	start := time.Now()
	results, err := Run(ctx, client, l, scenarios(2), Config{
		PeerAddr: recv.Addr().String(),
		Timeout:  time.Second,
		Interval: 10 * time.Second,
	})
	elapsed := time.Since(start)
	if err == nil {
		t.Error("Run() error = nil, want the cancellation")
	}
	if elapsed > time.Second {
		t.Errorf("Run() returned %v after start, want under 1s", elapsed)
	}
	if len(results) != 1 || results[0].Seconds <= 0 {
		t.Errorf("Run() results = %+v, want request 0's finished transfer only", results)
	}
	t.Logf("returned after %v: %v", elapsed, err)
}

// Canceling ctx during a transfer stops the run, and the cut-off transfer
// is left out of the results rather than recorded as a failure.
func TestCancelDuringTransferLeavesItOut(t *testing.T) {
	peer := startStuckPeer(t)
	client := startWorker(t, peer)
	l, err := learner.NewEWMA(0.5, 1e-9)
	if err != nil {
		t.Fatal(err)
	}
	big := scenarios(2)
	for i := range big {
		big[i].Request.PrefixTokens = 1 << 20 // 1 GiB, far more than the TCP buffers hold
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	results, err := Run(ctx, client, l, big, Config{PeerAddr: peer, Timeout: 10 * time.Second})
	if err == nil {
		t.Error("Run() error = nil, want the cancellation")
	}
	if len(results) != 0 {
		t.Errorf("Run() results = %+v, want none: the only transfer was cut off", results)
	}
}

// A transfer the worker rejects as misconfigured stops the run, since every
// later one would be rejected too.
func TestMisconfiguredTransferStops(t *testing.T) {
	for _, tc := range []struct {
		peer string
		want codes.Code
	}{
		{"127.0.0.1:1", codes.PermissionDenied}, // not in the worker's allowlist
		{"", codes.InvalidArgument},
	} {
		client, _ := startCluster(t)
		l, err := learner.NewEWMA(0.5, 1e-9)
		if err != nil {
			t.Fatal(err)
		}
		results, err := Run(context.Background(), client, l, scenarios(5), Config{PeerAddr: tc.peer, Timeout: time.Second})
		if status.Code(err) != tc.want {
			t.Errorf("peer %q: Run() error = %v, want %v", tc.peer, err, tc.want)
		}
		if len(results) != 0 {
			t.Errorf("peer %q: Run() returned %d results, want 0", tc.peer, len(results))
		}
	}
}
