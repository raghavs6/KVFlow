// Package controller runs KVFlow's decision loop against real workers.
// Transfers are real gRPC calls to a worker; queues and prefill speeds still
// come from simulated scenarios, since there is no real inference yet.
package controller

import (
	"context"
	"errors"
	"time"

	"github.com/raghavs6/KVFlow/internal/costmodel"
	"github.com/raghavs6/KVFlow/internal/scheduler"
	"github.com/raghavs6/KVFlow/internal/simulator"
	"github.com/raghavs6/KVFlow/internal/workerpb"
)

var (
	errInvalidProbe   = errors.New("probeEvery must not be negative")
	errInvalidTimeout = errors.New("timeout must be positive")
)

// Result is what happened to one request.
type Result struct {
	// At is when the request started, measured from the start of the run,
	// so results can be lined up with outside changes such as a slowed link.
	At time.Duration
	// Believed is the action the learner predicted was fastest; Action is
	// the one taken, which differs only when a probe forced a transfer.
	Believed, Action scheduler.Action
	// PredictedSeconds is how long the learner expected the prefix's
	// transfer to take, whether or not it ran.
	PredictedSeconds float64
	// Seconds is how long the transfer took as the worker measured it, or 0
	// if there was no transfer or it failed.
	Seconds float64
	// Err is why the transfer failed. A failed transfer teaches the learner
	// nothing.
	Err error
}

// Config holds the settings for a Run.
type Config struct {
	// PeerAddr is the data address the worker sends transfers to.
	PeerAddr string
	// ProbeEvery is how many requests may pass without a transfer before
	// the next one is forced to transfer; 0 disables probing.
	ProbeEvery int
	// Timeout is how long each transfer may take before it fails.
	Timeout time.Duration
}

// Run decides each scenario with l's current belief. When transfer wins, it
// asks the worker behind client to send the prefix's bytes to cfg.PeerAddr
// and teaches l the time the worker measured. Each scenario's bandwidth and
// startup are ignored: the real network is the truth. After cfg.ProbeEvery
// requests without a transfer, the next request transfers whatever l
// believes, so a wrong belief that the network is slow still gets measured.
// A failed transfer is recorded in its result and the run goes on; a failed
// attempt still counts as the probe, so a dead worker is tried at most once
// every ProbeEvery+1 requests. Each transfer gets cfg.Timeout to finish, so
// a stuck one fails instead of hanging the run. It returns a result for each
// scenario, and stops early only when ctx ends or cfg is invalid.
func Run(
	ctx context.Context,
	client workerpb.WorkerClient,
	l simulator.Learner,
	scenarios []simulator.Scenario,
	cfg Config,
) ([]Result, error) {
	if cfg.ProbeEvery < 0 {
		return nil, errInvalidProbe
	}
	if cfg.Timeout <= 0 {
		return nil, errInvalidTimeout
	}

	sinceTransfer := 0
	results := make([]Result, len(scenarios))
	start := time.Now()
	for i, s := range scenarios {
		at := time.Since(start)
		candidates, err := costmodel.Estimate(costmodel.Inputs{
			QueueA:               s.Source.Queue,
			QueueB:               s.Destination.Queue,
			PrefixTokens:         s.Request.PrefixTokens,
			SuffixTokens:         s.Request.SuffixTokens,
			PrefillTokensPerSecA: s.Source.PrefillTokensPerSec,
			PrefillTokensPerSecB: s.Destination.PrefillTokensPerSec,
			KVBytesPerToken:      s.KVBytesPerToken,
			BandwidthBytesPerSec: 1 / l.SecondsPerByte(),
			TransferStartup:      l.Startup(),
		})
		if err != nil {
			return nil, err
		}
		choice, err := scheduler.ChooseLowestTTFT(candidates)
		if err != nil {
			return nil, err
		}
		action := choice.Action
		if cfg.ProbeEvery > 0 && sinceTransfer >= cfg.ProbeEvery {
			action = scheduler.ActionTransfer
		}
		bytes := prefixBytes(s)
		results[i] = Result{
			At:               at,
			Believed:         choice.Action,
			Action:           action,
			PredictedSeconds: l.Startup().Seconds() + float64(bytes)*l.SecondsPerByte(),
		}

		sinceTransfer++
		if action != scheduler.ActionTransfer || bytes <= 0 {
			continue
		}
		sinceTransfer = 0
		callCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
		reply, err := client.Transfer(callCtx, &workerpb.TransferRequest{PeerAddr: cfg.PeerAddr, Bytes: bytes})
		cancel()
		if err != nil {
			// The worker's failure is one request's outcome; ctx ending
			// means the caller wants the run stopped.
			if ctx.Err() != nil {
				return nil, err
			}
			results[i].Err = err
			continue
		}
		results[i].Seconds = reply.GetSeconds()
		l.Observe(float64(bytes), reply.GetSeconds())
	}
	return results, nil
}

// prefixBytes is the size of s's KV prefix.
func prefixBytes(s simulator.Scenario) int64 {
	return int64(float64(s.Request.PrefixTokens) * s.KVBytesPerToken)
}
