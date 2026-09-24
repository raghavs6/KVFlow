package simulator

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/raghavs6/KVFlow/internal/scheduler"
)

func TestDecide(t *testing.T) {
	tests := []struct {
		name     string
		scenario Scenario
		want     scheduler.Candidate
	}{
		{
			name:     "fast network selects transfer",
			scenario: baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000),
			want: scheduler.Candidate{
				Action:        scheduler.ActionTransfer,
				EstimatedTTFT: 220 * time.Millisecond,
			},
		},
		{
			name:     "congested network and busy source select recompute",
			scenario: baseScenario(3*time.Second, 50*time.Millisecond, 1_000_000_000),
			want: scheduler.Candidate{
				Action:        scheduler.ActionRecompute,
				EstimatedTTFT: 1070 * time.Millisecond,
			},
		},
		{
			name:     "short source queue selects wait",
			scenario: baseScenario(100*time.Millisecond, 300*time.Millisecond, 10_000_000_000),
			want: scheduler.Candidate{
				Action:        scheduler.ActionWait,
				EstimatedTTFT: 120 * time.Millisecond,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decide(tt.scenario)
			if err != nil {
				t.Fatalf("Decide() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("Decide() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDecideRejectsInvalidScenario(t *testing.T) {
	scenario := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
	scenario.BandwidthBytesPerSec = 0

	if _, err := Decide(scenario); err == nil {
		t.Fatal("Decide() error = nil, want an invalid-input error")
	}
}

func TestActualTTFTUsesTruthNotBelief(t *testing.T) {
	belief := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
	truth := baseScenario(400*time.Millisecond, 50*time.Millisecond, 1_000_000_000)

	choice, err := Decide(belief)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if choice.Action != scheduler.ActionTransfer || choice.EstimatedTTFT != 220*time.Millisecond {
		t.Fatalf("Decide() = %+v, want transfer predicted at 220ms", choice)
	}

	got, err := ActualTTFT(truth, choice.Action)
	if err != nil {
		t.Fatalf("ActualTTFT() error = %v", err)
	}
	if want := 2020 * time.Millisecond; got != want {
		t.Fatalf("ActualTTFT() = %v, want %v", got, want)
	}
}

func TestActualTTFTRejectsUnknownAction(t *testing.T) {
	truth := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)

	if _, err := ActualTTFT(truth, scheduler.Action("teleport")); err == nil {
		t.Fatal("ActualTTFT() error = nil, want an unknown-action error")
	}
}

func TestRegret(t *testing.T) {
	fast := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
	congested := baseScenario(400*time.Millisecond, 50*time.Millisecond, 1_000_000_000)

	tests := []struct {
		name   string
		belief Scenario
		truth  Scenario
		want   time.Duration
	}{
		{name: "stale belief pays for transfer over wait", belief: fast, truth: congested, want: 1600 * time.Millisecond},
		{name: "accurate belief has no regret", belief: congested, truth: congested, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			choice, err := Decide(tt.belief)
			if err != nil {
				t.Fatalf("Decide() error = %v", err)
			}

			got, err := Regret(tt.truth, choice.Action)
			if err != nil {
				t.Fatalf("Regret() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("Regret() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegretRejectsUnknownAction(t *testing.T) {
	truth := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)

	if _, err := Regret(truth, scheduler.Action("teleport")); err == nil {
		t.Fatal("Regret() error = nil, want an unknown-action error")
	}
}

func TestRunStaticKeepsPayingAfterCongestion(t *testing.T) {
	fast := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
	congested := baseScenario(400*time.Millisecond, 50*time.Millisecond, 1_000_000_000)
	truths := []Scenario{fast, fast, fast, congested, congested, congested}

	got, err := RunStatic(fast.BandwidthBytesPerSec, truths)
	if err != nil {
		t.Fatalf("RunStatic() error = %v", err)
	}

	want := []time.Duration{0, 0, 0, 1600 * time.Millisecond, 1600 * time.Millisecond, 1600 * time.Millisecond}
	if len(got) != len(want) {
		t.Fatalf("RunStatic() returned %d regrets, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Regret != want[i] {
			t.Errorf("RunStatic()[%d] = %v, want %v", i, got[i].Regret, want[i])
		}
	}
}

func TestRunStaticEmptySequence(t *testing.T) {
	belief := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)

	got, err := RunStatic(belief.BandwidthBytesPerSec, nil)
	if err != nil {
		t.Fatalf("RunStatic() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("RunStatic() = %v, want no regrets", got)
	}
}

func TestRunStaticRejectsInvalidTruth(t *testing.T) {
	belief := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
	invalid := belief
	invalid.BandwidthBytesPerSec = 0

	if _, err := RunStatic(belief.BandwidthBytesPerSec, []Scenario{belief, invalid}); err == nil {
		t.Fatal("RunStatic() error = nil, want an invalid-input error")
	}
}

func TestRunAdaptiveRecoversAfterCongestion(t *testing.T) {
	fast := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
	congested := baseScenario(400*time.Millisecond, 50*time.Millisecond, 1_000_000_000)
	truths := []Scenario{fast, fast, fast, congested, congested, congested}

	tests := []struct {
		name  string
		alpha float64
		want  []time.Duration
	}{
		{name: "alpha 0.5 adapts after one bad transfer", alpha: 0.5, want: []time.Duration{0, 0, 0, 1600 * time.Millisecond, 0, 0}},
		{name: "alpha 0.1 adapts after two bad transfers", alpha: 0.1, want: []time.Duration{0, 0, 0, 1600 * time.Millisecond, 1600 * time.Millisecond, 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RunAdaptive(10_000_000_000, tt.alpha, 0, exact, truths)
			if err != nil {
				t.Fatalf("RunAdaptive() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("RunAdaptive() returned %d regrets, want %d", len(got), len(tt.want))
			}
			for i := range tt.want {
				if got[i].Regret != tt.want[i] {
					t.Errorf("RunAdaptive()[%d] = %v, want %v", i, got[i].Regret, tt.want[i])
				}
			}
		})
	}
}

// This pins down a known weakness: after switching to wait, the adaptive run
// never transfers again, so it never observes that the network recovered.
func TestRunAdaptiveMissesRecoveryWithoutExploration(t *testing.T) {
	fast := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
	congested := baseScenario(400*time.Millisecond, 50*time.Millisecond, 1_000_000_000)
	truths := []Scenario{fast, fast, congested, congested, fast, fast, fast}

	got, err := RunAdaptive(10_000_000_000, 0.5, 0, exact, truths)
	if err != nil {
		t.Fatalf("RunAdaptive() error = %v", err)
	}

	want := []time.Duration{
		0, 0, 1600 * time.Millisecond, 0,
		200 * time.Millisecond, 200 * time.Millisecond, 200 * time.Millisecond,
	}
	if len(got) != len(want) {
		t.Fatalf("RunAdaptive() returned %d regrets, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Regret != want[i] {
			t.Errorf("RunAdaptive()[%d] = %v, want %v", i, got[i].Regret, want[i])
		}
	}
}

func TestRunAdaptiveRejectsInvalidAlpha(t *testing.T) {
	truths := []Scenario{baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)}

	for _, alpha := range []float64{0, -0.5, 1.5} {
		if _, err := RunAdaptive(10_000_000_000, alpha, 0, exact, truths); err == nil {
			t.Errorf("RunAdaptive(alpha=%v) error = nil, want an invalid-alpha error", alpha)
		}
	}
}

func TestRunAdaptiveProbing(t *testing.T) {
	fast := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
	congested := baseScenario(400*time.Millisecond, 50*time.Millisecond, 1_000_000_000)
	const ms = time.Millisecond

	tests := []struct {
		name   string
		truths []Scenario
		want   []time.Duration
	}{
		{
			// Probes at requests 6, 9 and 12 pull the belief back toward
			// 10 GB/s; transfer wins again at request 13.
			name: "probes rediscover a recovered network",
			truths: []Scenario{
				fast, fast, congested, congested,
				fast, fast, fast, fast, fast, fast, fast, fast, fast,
			},
			want: []time.Duration{
				0, 0, 1600 * ms, 0,
				200 * ms, 0, 200 * ms, 200 * ms, 0, 200 * ms, 200 * ms, 0, 0,
			},
		},
		{
			// The probe at request 5 pays full regret because the network
			// is still congested.
			name:   "probes cost regret while congestion persists",
			truths: []Scenario{fast, congested, congested, congested, congested, congested},
			want:   []time.Duration{0, 1600 * ms, 0, 0, 1600 * ms, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RunAdaptive(10_000_000_000, 0.5, 2, exact, tt.truths)
			if err != nil {
				t.Fatalf("RunAdaptive() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("RunAdaptive() returned %d regrets, want %d", len(got), len(tt.want))
			}
			for i := range tt.want {
				if got[i].Regret != tt.want[i] {
					t.Errorf("RunAdaptive()[%d] = %v, want %v", i, got[i].Regret, tt.want[i])
				}
			}
		})
	}
}

// A probe executes a transfer without changing what the policy believes is
// best, so adaptation can be measured from belief rather than from probes.
func TestRunAdaptiveRecordsBeliefSeparatelyFromProbes(t *testing.T) {
	fast := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
	congested := baseScenario(400*time.Millisecond, 50*time.Millisecond, 1_000_000_000)
	truths := []Scenario{
		fast, fast, congested, congested,
		fast, fast, fast, fast, fast, fast, fast, fast, fast,
	}
	const (
		T = scheduler.ActionTransfer
		W = scheduler.ActionWait
	)

	outcomes, err := RunAdaptive(10_000_000_000, 0.5, 2, exact, truths)
	if err != nil {
		t.Fatalf("RunAdaptive() error = %v", err)
	}

	// Requests 6, 9 and 12 are probes: they transfer (zero regret on the
	// recovered network) while the belief is still wait.
	want := []scheduler.Action{T, T, T, W, W, W, W, W, W, W, W, W, T}
	if len(outcomes) != len(want) {
		t.Fatalf("RunAdaptive() returned %d outcomes, want %d", len(outcomes), len(want))
	}
	for i := range want {
		if outcomes[i].Believed != want[i] {
			t.Errorf("RunAdaptive()[%d].Believed = %s, want %s", i, outcomes[i].Believed, want[i])
		}
	}
	if outcomes[5].Regret != 0 {
		t.Errorf("RunAdaptive()[5].Regret = %v, want 0 because the probe transferred", outcomes[5].Regret)
	}
}

func TestRunStaticBelievesOneAction(t *testing.T) {
	fast := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
	congested := baseScenario(400*time.Millisecond, 50*time.Millisecond, 1_000_000_000)

	outcomes, err := RunStatic(fast.BandwidthBytesPerSec, []Scenario{fast, congested})
	if err != nil {
		t.Fatalf("RunStatic() error = %v", err)
	}
	for i, o := range outcomes {
		if o.Believed != scheduler.ActionTransfer {
			t.Errorf("RunStatic()[%d].Believed = %s, want transfer", i, o.Believed)
		}
	}
}

func TestRunAdaptiveRejectsNegativeProbeEvery(t *testing.T) {
	truths := []Scenario{baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)}

	if _, err := RunAdaptive(10_000_000_000, 0.5, -1, exact, truths); err == nil {
		t.Fatal("RunAdaptive(probeEvery=-1) error = nil, want an invalid-probe error")
	}
}

func TestRunAdaptiveMisledByNoisyObservation(t *testing.T) {
	fast := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
	fourTimesSlow := func(secondsPerByte float64) float64 { return 4 * secondsPerByte }

	// The network never changes, but one bad report moves the belief to
	// 0.25 ns/B (transfer looks like 520ms), so wait wins from then on.
	got, err := RunAdaptive(10_000_000_000, 0.5, 0, fourTimesSlow, []Scenario{fast, fast, fast})
	if err != nil {
		t.Fatalf("RunAdaptive() error = %v", err)
	}

	want := []time.Duration{0, 200 * time.Millisecond, 200 * time.Millisecond}
	if len(got) != len(want) {
		t.Fatalf("RunAdaptive() returned %d regrets, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Regret != want[i] {
			t.Errorf("RunAdaptive()[%d] = %v, want %v", i, got[i].Regret, want[i])
		}
	}
}

func TestNoisyObserveIsSeededAndBounded(t *testing.T) {
	const spread = 0.3
	first, err := NoisyObserve(rand.New(rand.NewPCG(1, 2)), spread)
	if err != nil {
		t.Fatalf("NoisyObserve() error = %v", err)
	}
	second, err := NoisyObserve(rand.New(rand.NewPCG(1, 2)), spread)
	if err != nil {
		t.Fatalf("NoisyObserve() error = %v", err)
	}

	const truth = 1e-9
	sawNoise := false
	for i := range 1000 {
		a, b := first(truth), second(truth)
		if a != b {
			t.Fatalf("report %d differs across equal seeds: %v vs %v", i, a, b)
		}
		if a < truth*(1-spread) || a > truth*(1+spread) {
			t.Fatalf("report %d = %v, outside ±%v of %v", i, a, spread, truth)
		}
		if a != truth {
			sawNoise = true
		}
	}
	if !sawNoise {
		t.Fatal("every report equaled the truth, want noise")
	}
}

func TestNoisyObserveRejectsInvalidSpread(t *testing.T) {
	for _, spread := range []float64{-0.1, 1, 1.5} {
		if _, err := NoisyObserve(rand.New(rand.NewPCG(1, 2)), spread); err == nil {
			t.Errorf("NoisyObserve(spread=%v) error = nil, want an invalid-spread error", spread)
		}
	}
}

// exact reports transfer measurements without noise.
func exact(secondsPerByte float64) float64 { return secondsPerByte }

func baseScenario(sourceQueue, destinationQueue time.Duration, bandwidth float64) Scenario {
	return Scenario{
		Source: Worker{
			Queue:               sourceQueue,
			PrefillTokensPerSec: 50_000,
		},
		Destination: Worker{
			Queue:               destinationQueue,
			PrefillTokensPerSec: 50_000,
		},
		Request: Request{
			PrefixTokens: 50_000,
			SuffixTokens: 1_000,
		},
		KVBytesPerToken:      40_000,
		BandwidthBytesPerSec: bandwidth,
	}
}

// withStartup returns a fast-network scenario whose transfers pay a startup
// delay the cost model cannot see. With a 1 s delay the true costs are wait
// 1520ms, transfer 1220ms, recompute 1070ms, while the model still predicts
// transfer at 220ms.
func withStartup(startup time.Duration) Scenario {
	s := baseScenario(1500*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
	s.TransferStartup = startup
	return s
}

func TestHiddenStartupAffectsTruthNotPrediction(t *testing.T) {
	truth := withStartup(time.Second)

	choice, err := Decide(truth)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if choice.Action != scheduler.ActionTransfer || choice.EstimatedTTFT != 220*time.Millisecond {
		t.Fatalf("Decide() = %+v, want transfer predicted at 220ms", choice)
	}

	actual, err := ActualTTFT(truth, scheduler.ActionTransfer)
	if err != nil {
		t.Fatalf("ActualTTFT() error = %v", err)
	}
	if want := 1220 * time.Millisecond; actual != want {
		t.Errorf("ActualTTFT(transfer) = %v, want %v", actual, want)
	}

	regret, err := Regret(truth, scheduler.ActionTransfer)
	if err != nil {
		t.Fatalf("Regret() error = %v", err)
	}
	if want := 150 * time.Millisecond; regret != want {
		t.Errorf("Regret(transfer) = %v, want %v against recompute", regret, want)
	}
}

// Startup overlaps the destination queue just like the transfer itself.
func TestHiddenStartupOverlapsDestinationQueue(t *testing.T) {
	truth := withStartup(0)
	truth.Destination.Queue = 500 * time.Millisecond
	truth.TransferStartup = 100 * time.Millisecond

	actual, err := ActualTTFT(truth, scheduler.ActionTransfer)
	if err != nil {
		t.Fatalf("ActualTTFT() error = %v", err)
	}
	// max(500ms queue, 100ms + 200ms transfer) + 20ms suffix.
	if want := 520 * time.Millisecond; actual != want {
		t.Errorf("ActualTTFT(transfer) = %v, want %v", actual, want)
	}
}

// Workers report the real transfer time, startup included, so repeated
// underpredictions pull the learned bandwidth down until transfer stops
// winning. Each report is 1.1s / 2GB; predicted transfer TTFT climbs
// 220, 670, 895, 1007.5, 1063.75, then 1091.9ms loses to 1070ms recompute.
func TestRunAdaptiveLearnsHiddenStartup(t *testing.T) {
	truths := Stable(7, withStartup(900*time.Millisecond))

	got, err := RunAdaptive(10_000_000_000, 0.5, 0, exact, truths)
	if err != nil {
		t.Fatalf("RunAdaptive() error = %v", err)
	}

	const ms = time.Millisecond
	want := []time.Duration{50 * ms, 50 * ms, 50 * ms, 50 * ms, 50 * ms, 0, 0}
	if len(got) != len(want) {
		t.Fatalf("RunAdaptive() returned %d outcomes, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Regret != want[i] {
			t.Errorf("RunAdaptive()[%d].Regret = %v, want %v", i, got[i].Regret, want[i])
		}
	}
}

func TestRunAdaptiveRecordsPredictedAndActual(t *testing.T) {
	fast := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
	congested := baseScenario(400*time.Millisecond, 50*time.Millisecond, 1_000_000_000)
	const ms = time.Millisecond

	tests := []struct {
		name       string
		probeEvery int
		truths     []Scenario
		index      int
		want       Outcome
	}{
		{
			name:   "hidden startup makes the first transfer underpredict",
			truths: Stable(2, withStartup(900*ms)),
			index:  0,
			want:   Outcome{Regret: 50 * ms, Believed: scheduler.ActionTransfer, Executed: scheduler.ActionTransfer, Predicted: 220 * ms, Actual: 1120 * ms},
		},
		{
			name:   "one report raises the next prediction",
			truths: Stable(2, withStartup(900*ms)),
			index:  1,
			want:   Outcome{Regret: 50 * ms, Believed: scheduler.ActionTransfer, Executed: scheduler.ActionTransfer, Predicted: 670 * ms, Actual: 1120 * ms},
		},
		{
			// The belief is 5.5e-10 s/B after one congested report, so
			// the probe's transfer is predicted at 1120ms but the network
			// has recovered.
			name:       "a probe predicts the transfer it executes",
			probeEvery: 2,
			truths:     []Scenario{fast, fast, congested, congested, fast, fast},
			index:      5,
			want:       Outcome{Regret: 0, Believed: scheduler.ActionWait, Executed: scheduler.ActionTransfer, Predicted: 1120 * ms, Actual: 220 * ms},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RunAdaptive(10_000_000_000, 0.5, tt.probeEvery, exact, tt.truths)
			if err != nil {
				t.Fatalf("RunAdaptive() error = %v", err)
			}
			if got[tt.index] != tt.want {
				t.Errorf("RunAdaptive()[%d] = %+v, want %+v", tt.index, got[tt.index], tt.want)
			}
		})
	}
}

func TestRunStaticRecordsPredictedAndActual(t *testing.T) {
	got, err := RunStatic(10_000_000_000, []Scenario{withStartup(900 * time.Millisecond)})
	if err != nil {
		t.Fatalf("RunStatic() error = %v", err)
	}
	want := Outcome{
		Regret:    50 * time.Millisecond,
		Believed:  scheduler.ActionTransfer,
		Executed:  scheduler.ActionTransfer,
		Predicted: 220 * time.Millisecond,
		Actual:    1120 * time.Millisecond,
	}
	if got[0] != want {
		t.Errorf("RunStatic()[0] = %+v, want %+v", got[0], want)
	}
}

// Static keeps its calibrated bandwidth but reads each request's own size
// and queues, so a 2k-token transfer is predicted at max(50ms, 8ms) + 20ms.
func TestRunStaticUsesEachRequest(t *testing.T) {
	small := withStartup(200 * time.Millisecond)
	small.Request.PrefixTokens = 2_000

	got, err := RunStatic(10_000_000_000, []Scenario{small})
	if err != nil {
		t.Fatalf("RunStatic() error = %v", err)
	}
	// True costs: transfer max(50ms, 208ms) + 20ms = 228ms, recompute 110ms.
	want := Outcome{
		Regret:    118 * time.Millisecond,
		Believed:  scheduler.ActionTransfer,
		Executed:  scheduler.ActionTransfer,
		Predicted: 70 * time.Millisecond,
		Actual:    228 * time.Millisecond,
	}
	if got[0] != want {
		t.Errorf("RunStatic()[0] = %+v, want %+v", got[0], want)
	}
}
