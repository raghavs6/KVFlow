// Package simulator turns simulated cluster snapshots into scheduling decisions.
package simulator

import (
	"time"

	"github.com/raghavs6/KVFlow/internal/costmodel"
	"github.com/raghavs6/KVFlow/internal/scheduler"
)

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
	candidates, err := costmodel.Estimate(costmodel.Inputs{
		QueueA:               scenario.Source.Queue,
		QueueB:               scenario.Destination.Queue,
		PrefixTokens:         scenario.Request.PrefixTokens,
		SuffixTokens:         scenario.Request.SuffixTokens,
		PrefillTokensPerSecA: scenario.Source.PrefillTokensPerSec,
		PrefillTokensPerSecB: scenario.Destination.PrefillTokensPerSec,
		KVBytesPerToken:      scenario.KVBytesPerToken,
		BandwidthBytesPerSec: scenario.BandwidthBytesPerSec,
	})
	if err != nil {
		return scheduler.Candidate{}, err
	}

	return scheduler.ChooseLowestTTFT(candidates)
}
