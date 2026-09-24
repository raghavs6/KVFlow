// Package costmodel predicts time to first token for execution plans.
package costmodel

import (
	"errors"
	"math"
	"time"

	"github.com/raghavs6/KVFlow/internal/scheduler"
)

var errInvalidInputs = errors.New("invalid cost model inputs")

// Inputs contains the measurements needed to estimate each execution plan.
type Inputs struct {
	QueueA, QueueB                             time.Duration
	PrefixTokens, SuffixTokens                 int
	PrefillTokensPerSecA, PrefillTokensPerSecB float64
	KVBytesPerToken, BandwidthBytesPerSec      float64
	// TransferStartup is a fixed delay every transfer pays before bytes
	// flow, such as connection setup.
	TransferStartup time.Duration
}

// Estimate returns wait, transfer, and recompute candidates in tie-break order.
func Estimate(inputs Inputs) ([]scheduler.Candidate, error) {
	if inputs.QueueA < 0 || inputs.QueueB < 0 || inputs.TransferStartup < 0 ||
		inputs.PrefixTokens < 0 || inputs.SuffixTokens < 0 ||
		!isFinitePositive(inputs.PrefillTokensPerSecA) ||
		!isFinitePositive(inputs.PrefillTokensPerSecB) ||
		!isFinitePositive(inputs.KVBytesPerToken) ||
		!isFinitePositive(inputs.BandwidthBytesPerSec) {
		return nil, errInvalidInputs
	}

	suffixA := durationFor(float64(inputs.SuffixTokens), inputs.PrefillTokensPerSecA)
	suffixB := durationFor(float64(inputs.SuffixTokens), inputs.PrefillTokensPerSecB)
	transferTime := inputs.TransferStartup + durationFor(
		float64(inputs.PrefixTokens)*inputs.KVBytesPerToken,
		inputs.BandwidthBytesPerSec,
	)
	prefixAndSuffixB := durationFor(
		float64(inputs.PrefixTokens)+float64(inputs.SuffixTokens),
		inputs.PrefillTokensPerSecB,
	)

	transferStart := max(inputs.QueueB, transferTime)
	return []scheduler.Candidate{
		{Action: scheduler.ActionWait, EstimatedTTFT: inputs.QueueA + suffixA},
		{Action: scheduler.ActionTransfer, EstimatedTTFT: transferStart + suffixB},
		{Action: scheduler.ActionRecompute, EstimatedTTFT: inputs.QueueB + prefixAndSuffixB},
	}, nil
}

func durationFor(amount, rate float64) time.Duration {
	return time.Duration(amount / rate * float64(time.Second))
}

func isFinitePositive(value float64) bool {
	return value > 0 && !math.IsInf(value, 0) && !math.IsNaN(value)
}
