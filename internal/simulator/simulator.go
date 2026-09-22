// Package simulator turns simulated cluster snapshots into scheduling decisions.
package simulator

import (
	"errors"
	"time"

	"github.com/raghavs6/KVFlow/internal/costmodel"
	"github.com/raghavs6/KVFlow/internal/scheduler"
)

var errUnknownAction = errors.New("unknown action")

// Request describes the token work needed before generation can begin.
type Request struct {
	PrefixTokens int
	SuffixTokens int
}

// Worker is a point-in-time view of a simulated inference worker.
type Worker struct {
	Queue               time.Duration
	PrefillTokensPerSec float64
}

// Scenario contains one prefix-owning source, one destination, and their link.
type Scenario struct {
	Source               Worker
	Destination          Worker
	Request              Request
	KVBytesPerToken      float64
	BandwidthBytesPerSec float64
}

// Decide predicts each feasible plan and returns the one with the lowest TTFT.
func Decide(scenario Scenario) (scheduler.Candidate, error) {
	candidates, err := estimate(scenario)
	if err != nil {
		return scheduler.Candidate{}, err
	}

	return scheduler.ChooseLowestTTFT(candidates)
}

// ActualTTFT returns how long action really takes under the true scenario.
// Truth reuses the cost model's formula, so the only way a decision can be
// wrong is that it was made from a stale belief about the scenario.
func ActualTTFT(truth Scenario, action scheduler.Action) (time.Duration, error) {
	candidates, err := estimate(truth)
	if err != nil {
		return 0, err
	}

	for _, candidate := range candidates {
		if candidate.Action == action {
			return candidate.EstimatedTTFT, nil
		}
	}
	return 0, errUnknownAction
}

func estimate(scenario Scenario) ([]scheduler.Candidate, error) {
	return costmodel.Estimate(costmodel.Inputs{
		QueueA:               scenario.Source.Queue,
		QueueB:               scenario.Destination.Queue,
		PrefixTokens:         scenario.Request.PrefixTokens,
		SuffixTokens:         scenario.Request.SuffixTokens,
		PrefillTokensPerSecA: scenario.Source.PrefillTokensPerSec,
		PrefillTokensPerSecB: scenario.Destination.PrefillTokensPerSec,
		KVBytesPerToken:      scenario.KVBytesPerToken,
		BandwidthBytesPerSec: scenario.BandwidthBytesPerSec,
	})
}
