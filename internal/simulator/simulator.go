// Package simulator turns simulated cluster snapshots into scheduling decisions.
package simulator

import (
	"errors"
	"math/rand/v2"
	"time"

	"github.com/raghavs6/KVFlow/internal/costmodel"
	"github.com/raghavs6/KVFlow/internal/scheduler"
)

var (
	errUnknownAction = errors.New("unknown action")
	errInvalidProbe  = errors.New("probeEvery must not be negative")
	errInvalidSpread = errors.New("spread must be in [0, 1)")
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
	// TransferStartup is a fixed delay paid by every transfer, such as
	// connection setup. It is part of the truth only: policies are never
	// told it, and predict with their own startup estimate instead.
	TransferStartup time.Duration
}

// Decide predicts each feasible plan and returns the one with the lowest TTFT.
// It assumes transfers have no startup.
func Decide(scenario Scenario) (scheduler.Candidate, error) {
	candidates, err := estimate(scenario, 0)
	if err != nil {
		return scheduler.Candidate{}, err
	}

	return scheduler.ChooseLowestTTFT(candidates)
}

// ActualTTFT returns how long action really takes under the true scenario.
// Truth is the cost model's formula with the true TransferStartup, so a
// decision can be wrong because its bandwidth or startup belief is.
func ActualTTFT(truth Scenario, action scheduler.Action) (time.Duration, error) {
	candidates, err := actualCosts(truth)
	if err != nil {
		return 0, err
	}
	return ttftOf(candidates, action)
}

func ttftOf(candidates []scheduler.Candidate, action scheduler.Action) (time.Duration, error) {
	for _, candidate := range candidates {
		if candidate.Action == action {
			return candidate.EstimatedTTFT, nil
		}
	}
	return 0, errUnknownAction
}

// Regret returns how much slower chosen really is than the choice a scheduler
// with perfect knowledge of truth would have made.
func Regret(truth Scenario, chosen scheduler.Action) (time.Duration, error) {
	actual, err := ActualTTFT(truth, chosen)
	if err != nil {
		return 0, err
	}

	best, err := bestActual(truth)
	if err != nil {
		return 0, err
	}
	return actual - best.EstimatedTTFT, nil
}

// bestActual returns the action a scheduler with perfect knowledge of truth,
// including costs the model cannot see, would choose.
func bestActual(truth Scenario) (scheduler.Candidate, error) {
	candidates, err := actualCosts(truth)
	if err != nil {
		return scheduler.Candidate{}, err
	}
	return scheduler.ChooseLowestTTFT(candidates)
}

// actualCosts returns every action's true TTFT, startup included.
func actualCosts(truth Scenario) ([]scheduler.Candidate, error) {
	return estimate(truth, truth.TransferStartup)
}

// transferDuration is how long moving the prefix's KV really takes.
func transferDuration(truth Scenario) time.Duration {
	bytes := float64(truth.Request.PrefixTokens) * truth.KVBytesPerToken
	return truth.TransferStartup + time.Duration(bytes/truth.BandwidthBytesPerSec*float64(time.Second))
}

// Outcome records one request of a run.
type Outcome struct {
	// Regret is how much slower the executed action was than the best one.
	Regret time.Duration
	// Believed is the action the policy's own estimates ranked best. It
	// differs from the executed action only when a probe forces a transfer.
	Believed scheduler.Action
	// Executed is the action that actually ran.
	Executed scheduler.Action
	// Predicted is the policy's estimated TTFT for Executed; Actual is what
	// it really took.
	Predicted, Actual time.Duration
}

// RunStatic scores a sequence of true scenarios with a bandwidth measured once
// at startup and never updated, like an offline cost model. Queues and the
// request are taken fresh from each scenario, as for RunAdaptive, so only the
// bandwidth is frozen. It returns the outcome of each request in order.
func RunStatic(bandwidth float64, truths []Scenario) ([]Outcome, error) {
	outcomes := make([]Outcome, len(truths))
	for i, truth := range truths {
		belief := truth
		belief.BandwidthBytesPerSec = bandwidth

		choice, err := Decide(belief)
		if err != nil {
			return nil, err
		}
		outcomes[i], err = record(belief, 0, truth, choice.Action, choice.Action)
		if err != nil {
			return nil, err
		}
	}
	return outcomes, nil
}

// Learner turns observed transfers into a belief about transfer cost.
type Learner interface {
	// Observe adds one transfer of bytes (> 0) that took seconds.
	Observe(bytes, seconds float64)
	Startup() time.Duration
	SecondsPerByte() float64
}

// RunAdaptive scores a sequence of true scenarios while l learns transfer
// cost from each executed transfer. Queues and the request are taken fresh
// from each scenario, as if reported by workers; only transfer cost is
// learned. After probeEvery requests without a transfer, the next request is
// forced to transfer so the network is measured again; 0 disables probing.
// observe turns a transfer's real duration into what the worker reports,
// which lets callers add measurement noise. It returns the outcome of each
// request in order.
func RunAdaptive(
	l Learner,
	probeEvery int,
	observe func(actualSeconds float64) float64,
	truths []Scenario,
) ([]Outcome, error) {
	if probeEvery < 0 {
		return nil, errInvalidProbe
	}

	sinceTransfer := 0
	outcomes := make([]Outcome, len(truths))
	for i, truth := range truths {
		belief := truth
		belief.BandwidthBytesPerSec = 1 / l.SecondsPerByte()
		startup := l.Startup()

		candidates, err := estimate(belief, startup)
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
		outcomes[i], err = record(belief, startup, truth, choice.Action, action)
		if err != nil {
			return nil, err
		}

		sinceTransfer++
		if action == scheduler.ActionTransfer {
			sinceTransfer = 0
			// The worker reports how long the transfer really took, so
			// costs the model gets wrong, like an unlearned startup, leak
			// into what it learns. An empty transfer says nothing.
			bytes := float64(truth.Request.PrefixTokens) * truth.KVBytesPerToken
			if bytes > 0 {
				l.Observe(bytes, observe(transferDuration(truth).Seconds()))
			}
		}
	}
	return outcomes, nil
}

// record scores one request where the policy, holding belief and a startup
// estimate, ranked believed best but ran executed.
func record(belief Scenario, startup time.Duration, truth Scenario, believed, executed scheduler.Action) (Outcome, error) {
	predictions, err := estimate(belief, startup)
	if err != nil {
		return Outcome{}, err
	}
	predicted, err := ttftOf(predictions, executed)
	if err != nil {
		return Outcome{}, err
	}
	actual, err := ActualTTFT(truth, executed)
	if err != nil {
		return Outcome{}, err
	}
	regret, err := Regret(truth, executed)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{
		Regret:    regret,
		Believed:  believed,
		Executed:  executed,
		Predicted: predicted,
		Actual:    actual,
	}, nil
}

// NoisyObserve returns an observe function for RunAdaptive that scales each
// actual transfer duration by a uniform factor in [1-spread, 1+spread]. The
// noise is unbiased on average, so it makes reports jittery without making
// the network look consistently faster or slower. Equal rng seeds give equal
// reports.
func NoisyObserve(rng *rand.Rand, spread float64) (func(float64) float64, error) {
	if !(spread >= 0 && spread < 1) {
		return nil, errInvalidSpread
	}
	return func(actualSeconds float64) float64 {
		return actualSeconds * (1 + spread*(2*rng.Float64()-1))
	}, nil
}

// estimate runs the cost model on scenario with the given startup, which is
// passed separately so a belief can never pick up the truth's startup.
func estimate(scenario Scenario, startup time.Duration) ([]scheduler.Candidate, error) {
	return costmodel.Estimate(costmodel.Inputs{
		QueueA:               scenario.Source.Queue,
		QueueB:               scenario.Destination.Queue,
		PrefixTokens:         scenario.Request.PrefixTokens,
		SuffixTokens:         scenario.Request.SuffixTokens,
		PrefillTokensPerSecA: scenario.Source.PrefillTokensPerSec,
		PrefillTokensPerSecB: scenario.Destination.PrefillTokensPerSec,
		KVBytesPerToken:      scenario.KVBytesPerToken,
		BandwidthBytesPerSec: scenario.BandwidthBytesPerSec,
		TransferStartup:      startup,
	})
}
