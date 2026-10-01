// Package controller runs KVFlow's decision loop against real workers.
// Transfers are real gRPC calls to a worker; queues and prefill speeds still
// come from simulated scenarios, since there is no real inference yet.
package controller

import (
	"context"

	"github.com/raghavs6/KVFlow/internal/costmodel"
	"github.com/raghavs6/KVFlow/internal/scheduler"
	"github.com/raghavs6/KVFlow/internal/simulator"
	"github.com/raghavs6/KVFlow/internal/workerpb"
)

// Run decides each scenario with l's current belief. When transfer wins, it
// asks the worker behind client to send the prefix's bytes to peerAddr and
// teaches l the time the worker measured. Each scenario's bandwidth and
// startup are ignored: the real network is the truth. It returns the action
// taken for each scenario, and stops at the first error.
func Run(
	ctx context.Context,
	client workerpb.WorkerClient,
	peerAddr string,
	l simulator.Learner,
	scenarios []simulator.Scenario,
) ([]scheduler.Action, error) {
	actions := make([]scheduler.Action, len(scenarios))
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
		actions[i] = choice.Action

		bytes := int64(float64(s.Request.PrefixTokens) * s.KVBytesPerToken)
		if choice.Action != scheduler.ActionTransfer || bytes <= 0 {
			continue
		}
		reply, err := client.Transfer(ctx, &workerpb.TransferRequest{PeerAddr: peerAddr, Bytes: bytes})
		if err != nil {
			return nil, err
		}
		l.Observe(float64(bytes), reply.GetSeconds())
	}
	return actions, nil
}
