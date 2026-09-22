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
