package simulator

import (
	"errors"

	"github.com/raghavs6/KVFlow/internal/scheduler"
)

var errLengthMismatch = errors.New("outcomes and truths must have the same length")

// Adaptation describes how a policy responded to one change in the true best
// action.
type Adaptation struct {
	// At is the index of the first request with the new best action.
	At       int
	From, To scheduler.Action
	// Requests counts the requests from At until the policy first believed
	// To was best. It is meaningful only when Adapted is true.
	Requests int
	// Adapted is false if the belief never matched To before the next change
	// point or the end of the run.
	Adapted bool
}

// AdaptationTimes finds every change point in truths and measures how long
// the policy that produced outcomes took to believe the new best action.
// Belief is used rather than the executed action so forced probes do not
// count as adapting.
func AdaptationTimes(truths []Scenario, outcomes []Outcome) ([]Adaptation, error) {
	if len(truths) != len(outcomes) {
		return nil, errLengthMismatch
	}

	best := make([]scheduler.Action, len(truths))
	for i, truth := range truths {
		choice, err := Decide(truth)
		if err != nil {
			return nil, err
		}
		best[i] = choice.Action
	}

	var adaptations []Adaptation
	for i := 1; i < len(best); i++ {
		if best[i] == best[i-1] {
			continue
		}
		a := Adaptation{At: i, From: best[i-1], To: best[i]}
		// The window ends where the best action changes again.
		for j := i; j < len(best) && best[j] == a.To; j++ {
			if outcomes[j].Believed == a.To {
				a.Requests = j - i
				a.Adapted = true
				break
			}
		}
		adaptations = append(adaptations, a)
	}
	return adaptations, nil
}
