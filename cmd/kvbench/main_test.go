package main

import (
	"testing"

	"github.com/raghavs6/KVFlow/internal/scheduler"
	"github.com/raghavs6/KVFlow/internal/simulator"
)

// The benchmark only exercises KVFlow's core question if each network phase
// has a different best action: transfer when fast, recompute when slow.
func TestScenarioBestActions(t *testing.T) {
	tests := []struct {
		name      string
		bandwidth float64
		want      scheduler.Action
	}{
		{name: "fast", bandwidth: fastBandwidth, want: scheduler.ActionTransfer},
		{name: "slow", bandwidth: slowBandwidth, want: scheduler.ActionRecompute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := simulator.Decide(scenario(tt.bandwidth))
			if err != nil {
				t.Fatalf("Decide() error = %v", err)
			}
			if got.Action != tt.want {
				t.Errorf("Decide() = %s, want %s", got.Action, tt.want)
			}
		})
	}
}

// With the hidden startup, the model still predicts transfer for every
// prefix size, but recompute is truly best for small prefixes. No single
// learned bandwidth can make both choices correct.
func TestMixedPrefixBestActions(t *testing.T) {
	tests := []struct {
		prefixTokens int
		wantTrue     scheduler.Action
	}{
		{prefixTokens: smallPrefixTokens, wantTrue: scheduler.ActionRecompute},
		{prefixTokens: prefixTokens, wantTrue: scheduler.ActionTransfer},
	}

	for _, tt := range tests {
		s := startupScenario()
		s.Request.PrefixTokens = tt.prefixTokens

		predicted, err := simulator.Decide(s)
		if err != nil {
			t.Fatalf("Decide() error = %v", err)
		}
		if predicted.Action != scheduler.ActionTransfer {
			t.Errorf("prefix %d: Decide() = %s, want transfer", tt.prefixTokens, predicted.Action)
		}
		// The truly best action has zero regret.
		regret, err := simulator.Regret(s, tt.wantTrue)
		if err != nil {
			t.Fatalf("Regret() error = %v", err)
		}
		if regret != 0 {
			t.Errorf("prefix %d: Regret(%s) = %v, want 0", tt.prefixTokens, tt.wantTrue, regret)
		}
	}
}
