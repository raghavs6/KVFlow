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
