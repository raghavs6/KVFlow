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
	errInvalidAlpha  = errors.New("alpha must be in (0, 1]")
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
	// connection setup. Only the simulated truth pays it; the cost model has
	// no term for it, so predictions ignore it.
	TransferStartup time.Duration
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
// Truth is the cost model's formula plus TransferStartup, so a decision can be
// wrong because its belief was stale or because the model's formula is.
func ActualTTFT(truth Scenario, action scheduler.Action) (time.Duration, error) {
	candidates, err := actualCosts(truth)
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

// actualCosts returns every action's true TTFT. It matches the cost model
// except that the transfer also pays TransferStartup, which overlaps the
// destination queue like the transfer itself.
func actualCosts(truth Scenario) ([]scheduler.Candidate, error) {
	candidates, err := estimate(truth)
	if err != nil {
		return nil, err
	}

	suffix := time.Duration(float64(truth.Request.SuffixTokens) / truth.Destination.PrefillTokensPerSec * float64(time.Second))
	for i := range candidates {
		if candidates[i].Action == scheduler.ActionTransfer {
			candidates[i].EstimatedTTFT = max(truth.Destination.Queue, transferDuration(truth)) + suffix
		}
	}
	return candidates, nil
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
}

// RunStatic scores a sequence of true scenarios against one belief that never
// updates, like a cost model calibrated once at startup. It returns the
// outcome of each request in order.
func RunStatic(belief Scenario, truths []Scenario) ([]Outcome, error) {
	choice, err := Decide(belief)
	if err != nil {
		return nil, err
	}

	outcomes := make([]Outcome, len(truths))
	for i, truth := range truths {
		regret, err := Regret(truth, choice.Action)
		if err != nil {
			return nil, err
		}
		outcomes[i] = Outcome{Regret: regret, Believed: choice.Action}
	}
	return outcomes, nil
}

// RunAdaptive scores a sequence of true scenarios while learning bandwidth
// from each executed transfer with an EWMA. Queues and prefill rates are
// taken fresh from each scenario, as if reported by workers; only bandwidth
// is learned. After probeEvery requests without a transfer, the next request
// is forced to transfer so bandwidth is measured again; 0 disables probing.
// observe turns a transfer's real duration per byte into what the worker
// reports, which lets callers add measurement noise. It returns the outcome of
// each request in order.
func RunAdaptive(
	initialBandwidth, alpha float64,
	probeEvery int,
	observe func(actualSecondsPerByte float64) float64,
	truths []Scenario,
) ([]Outcome, error) {
	if !(alpha > 0 && alpha <= 1) {
		return nil, errInvalidAlpha
	}
	if probeEvery < 0 {
		return nil, errInvalidProbe
	}

	// Average seconds per byte rather than bytes per second: transfer time is
	// linear in it, so the averaged estimate matches the average observed time.
	secondsPerByte := 1 / initialBandwidth
	sinceTransfer := 0
	outcomes := make([]Outcome, len(truths))
	for i, truth := range truths {
		belief := truth
		belief.BandwidthBytesPerSec = 1 / secondsPerByte

		choice, err := Decide(belief)
		if err != nil {
			return nil, err
		}
		action := choice.Action
		if probeEvery > 0 && sinceTransfer >= probeEvery {
			action = scheduler.ActionTransfer
		}
		regret, err := Regret(truth, action)
		if err != nil {
			return nil, err
		}
		outcomes[i] = Outcome{Regret: regret, Believed: choice.Action}

		sinceTransfer++
		if action == scheduler.ActionTransfer {
			sinceTransfer = 0
			// The worker reports how long the transfer really took per byte,
			// so costs the model has no term for, like startup, leak into
			// what it learns. observe decides how far the report is from
			// that. An empty transfer says nothing about bandwidth.
			bytes := float64(truth.Request.PrefixTokens) * truth.KVBytesPerToken
			if bytes > 0 {
				observed := observe(transferDuration(truth).Seconds() / bytes)
				secondsPerByte = (1-alpha)*secondsPerByte + alpha*observed
			}
		}
	}
	return outcomes, nil
}

// NoisyObserve returns an observe function for RunAdaptive that scales each
// actual seconds-per-byte by a uniform factor in [1-spread, 1+spread]. The
// noise is unbiased on average, so it makes reports jittery without making
// the network look consistently faster or slower. Equal rng seeds give equal
// reports.
func NoisyObserve(rng *rand.Rand, spread float64) (func(float64) float64, error) {
	if !(spread >= 0 && spread < 1) {
		return nil, errInvalidSpread
	}
	return func(actualSecondsPerByte float64) float64 {
		return actualSecondsPerByte * (1 + spread*(2*rng.Float64()-1))
	}, nil
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
