// Package scheduler chooses among feasible KV-cache execution plans.
package scheduler

import (
	"errors"
	"time"
)

// ErrNoCandidates means there is no feasible execution plan to choose.
var ErrNoCandidates = errors.New("no candidate execution plans")

// Action describes how KV state will be made available for a request.
type Action string

const (
	ActionWait      Action = "wait"
	ActionTransfer  Action = "transfer"
	ActionRecompute Action = "recompute"
)

// Candidate is a feasible action and its predicted time to first token.
type Candidate struct {
	Action        Action
	EstimatedTTFT time.Duration
}

// ChooseLowestTTFT returns the candidate with the smallest predicted TTFT.
// Ties preserve input order because the scheduler has no secondary objective.
func ChooseLowestTTFT(candidates []Candidate) (Candidate, error) {
	if len(candidates) == 0 {
		return Candidate{}, ErrNoCandidates
	}

	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.EstimatedTTFT < best.EstimatedTTFT {
			best = candidate
		}
	}

	return best, nil
}
