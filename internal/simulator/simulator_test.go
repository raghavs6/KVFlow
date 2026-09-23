package simulator

import (
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

	got, err := RunStatic(fast, truths)
	if err != nil {
		t.Fatalf("RunStatic() error = %v", err)
	}

	want := []time.Duration{0, 0, 0, 1600 * time.Millisecond, 1600 * time.Millisecond, 1600 * time.Millisecond}
	if len(got) != len(want) {
		t.Fatalf("RunStatic() returned %d regrets, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("RunStatic()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestRunStaticEmptySequence(t *testing.T) {
	belief := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)

	got, err := RunStatic(belief, nil)
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

	if _, err := RunStatic(belief, []Scenario{belief, invalid}); err == nil {
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
			got, err := RunAdaptive(10_000_000_000, tt.alpha, truths)
			if err != nil {
				t.Fatalf("RunAdaptive() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("RunAdaptive() returned %d regrets, want %d", len(got), len(tt.want))
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("RunAdaptive()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRunAdaptiveRejectsInvalidAlpha(t *testing.T) {
	truths := []Scenario{baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)}

	for _, alpha := range []float64{0, -0.5, 1.5} {
		if _, err := RunAdaptive(10_000_000_000, alpha, truths); err == nil {
			t.Errorf("RunAdaptive(alpha=%v) error = nil, want an invalid-alpha error", alpha)
		}
	}
}

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
