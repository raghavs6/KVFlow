// Package controller runs KVFlow's decision loop against real workers.
// Transfers are real gRPC calls to a worker; queues and prefill speeds still
// come from simulated scenarios, since there is no real inference yet.
package controller

import (
	"context"
	"errors"

	"github.com/raghavs6/KVFlow/internal/costmodel"
	"github.com/raghavs6/KVFlow/internal/scheduler"
	"github.com/raghavs6/KVFlow/internal/simulator"
	"github.com/raghavs6/KVFlow/internal/workerpb"
)

var errInvalidProbe = errors.New("probeEvery must not be negative")

// Result is what happened to one request.
type Result struct {
	// Believed is the action the learner predicted was fastest; Action is
	// the one taken, which differs only when a probe forced a transfer.
	Believed, Action scheduler.Action
	// PredictedSeconds is how long the learner expected the prefix's
	// transfer to take, whether or not it ran.
	PredictedSeconds float64
	// Seconds is how long the transfer took as the worker measured it, or 0
	// if there was no transfer.
	Seconds float64
}

// Run decides each scenario with l's current belief. When transfer wins, it
// asks the worker behind client to send the prefix's bytes to peerAddr and
// teaches l the time the worker measured. Each scenario's bandwidth and
// startup are ignored: the real network is the truth. After probeEvery
// requests without a transfer, the next request transfers whatever l
// believes, so a wrong belief that the network is slow still gets measured;
// 0 disables probing. It returns a result for each scenario, and stops at
// the first error.
func Run(
	ctx context.Context,
	client workerpb.WorkerClient,
	peerAddr string,
	l simulator.Learner,
	probeEvery int,
	scenarios []simulator.Scenario,
) ([]Result, error) {
	if probeEvery < 0 {
		return nil, errInvalidProbe
	}

	sinceTransfer := 0
	results := make([]Result, len(scenarios))
	for i, s := range scenarios {
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
		if probeEvery > 0 && sinceTransfer >= probeEvery {
			action = scheduler.ActionTransfer
		}
		bytes := int64(float64(s.Request.PrefixTokens) * s.KVBytesPerToken)
		results[i] = Result{
			Believed:         choice.Action,
			Action:           action,
			PredictedSeconds: l.Startup().Seconds() + float64(bytes)*l.SecondsPerByte(),
		}

		sinceTransfer++
		if action != scheduler.ActionTransfer || bytes <= 0 {
			continue
		}
		sinceTransfer = 0
		reply, err := client.Transfer(ctx, &workerpb.TransferRequest{PeerAddr: peerAddr, Bytes: bytes})
		if err != nil {
			return nil, err
		}
		results[i].Seconds = reply.GetSeconds()
		l.Observe(float64(bytes), reply.GetSeconds())
	}
	return results, nil
}
