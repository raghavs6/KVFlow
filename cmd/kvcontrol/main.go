// Command kvcontrol runs KVFlow's controller against a real kvworker. Each
// request is decided with the learner's current belief; transfers are real,
// sent by the worker to -peer, while recompute and wait are simulated. It
// writes one CSV row per request to stdout. Ctrl-C ends the run early and
// still writes the rows of the requests that finished.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/raghavs6/KVFlow/internal/controller"
	"github.com/raghavs6/KVFlow/internal/learner"
	"github.com/raghavs6/KVFlow/internal/simulator"
	"github.com/raghavs6/KVFlow/internal/workerpb"
)

// initialSecondsPerByte is kvbench's starting belief, a 10 GB/s network, so
// the controller starts from the same place as the simulated policies.
const initialSecondsPerByte = 1.0 / 10e9

// Usage:
//
//	kvcontrol -peer host:port [-worker 127.0.0.1:50051] [-learner line|ewma]
//	          [-alpha 0.5] [-probe 10] [-timeout 10s] [-interval 100ms]
//	          [-duration 3m] [-bytes 67108864] [-recompute 50ms]
//
// Transfer beats recompute when bandwidth is above bytes/recompute, 1.3 GB/s
// by default; pick the two so that point sits between the link's fast and
// slowed rates.
func main() {
	workerAddr := flag.String("worker", "127.0.0.1:50051", "kvworker's gRPC address")
	peer := flag.String("peer", "", "data address the worker sends transfers to; must be in its -peers")
	kind := flag.String("learner", "line", "line or ewma")
	alpha := flag.Float64("alpha", 0.5, "how much each transfer moves the learner's belief, in (0, 1]")
	probe := flag.Int("probe", 10, "force a transfer after this many requests without one; 0 disables")
	timeout := flag.Duration("timeout", 10*time.Second, "how long each transfer may take before it fails")
	interval := flag.Duration("interval", 100*time.Millisecond, "time between request starts")
	duration := flag.Duration("duration", 3*time.Minute, "how long to run")
	bytes := flag.Int64("bytes", 64<<20, "size of the KV prefix each request needs")
	recompute := flag.Duration("recompute", 50*time.Millisecond, "how long recomputing the prefix takes")
	flag.Parse()

	if *peer == "" {
		log.Fatal("kvcontrol: -peer is required")
	}
	if *interval <= 0 {
		log.Fatal("kvcontrol: -interval must be positive")
	}
	var l simulator.Learner
	var err error
	switch *kind {
	case "line":
		l, err = learner.NewLine(*alpha, initialSecondsPerByte)
	case "ewma":
		l, err = learner.NewEWMA(*alpha, initialSecondsPerByte)
	default:
		log.Fatalf("kvcontrol: -learner must be line or ewma, got %q", *kind)
	}
	if err != nil {
		log.Fatal(err)
	}

	conn, err := grpc.NewClient(*workerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	scenarios := simulator.Stable(int(*duration / *interval), scenario(*bytes, *recompute))
	log.Printf("kvcontrol: %d requests, one every %v, via %s to %s", len(scenarios), *interval, *workerAddr, *peer)
	results, err := controller.Run(ctx, workerpb.NewWorkerClient(conn), l, scenarios, controller.Config{
		PeerAddr:   *peer,
		ProbeEvery: *probe,
		Timeout:    *timeout,
		Interval:   *interval,
	})
	// Ctrl-C is how a run is ended by hand; write what finished.
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
	if err := controller.WriteCSV(os.Stdout, scenarios, results); err != nil {
		log.Fatal(err)
	}
	log.Printf("kvcontrol: wrote %d of %d requests", len(results), len(scenarios))
}

// scenario returns a request whose prefix is bytes of KV, which takes
// recompute to rebuild on the destination. The prefix is one token, so the
// cost model compares bytes over bandwidth directly with recompute. The
// source's hour-long queue keeps waiting from ever winning.
func scenario(bytes int64, recompute time.Duration) simulator.Scenario {
	return simulator.Scenario{
		Source:          simulator.Worker{Queue: time.Hour, PrefillTokensPerSec: 1},
		Destination:     simulator.Worker{PrefillTokensPerSec: 1 / recompute.Seconds()},
		Request:         simulator.Request{PrefixTokens: 1},
		KVBytesPerToken: float64(bytes),
	}
}
