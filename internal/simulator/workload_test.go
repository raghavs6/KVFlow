package simulator

import (
	"testing"
	"time"
)

func TestWorkloads(t *testing.T) {
	fast := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
	slow := baseScenario(400*time.Millisecond, 50*time.Millisecond, 1_000_000_000)
	F, S := fast.BandwidthBytesPerSec, slow.BandwidthBytesPerSec

	flapping, err := Flapping(7, 2, fast, slow)
	if err != nil {
		t.Fatalf("Flapping() error = %v", err)
	}

	tests := []struct {
		name string
		got  []Scenario
		want []float64
	}{
		{name: "stable", got: Stable(3, fast), want: []float64{F, F, F}},
		{name: "slowdown then recovery", got: SlowdownThenRecovery(6, fast, slow), want: []float64{F, F, S, S, F, F}},
		{name: "flapping", got: flapping, want: []float64{F, F, S, S, F, F, S}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.got) != len(tt.want) {
				t.Fatalf("got %d scenarios, want %d", len(tt.got), len(tt.want))
			}
			for i := range tt.want {
				if tt.got[i].BandwidthBytesPerSec != tt.want[i] {
					t.Errorf("scenario %d bandwidth = %v, want %v", i, tt.got[i].BandwidthBytesPerSec, tt.want[i])
				}
			}
		})
	}
}

func TestFlappingRejectsNonPositivePeriod(t *testing.T) {
	fast := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)

	if _, err := Flapping(4, 0, fast, fast); err == nil {
		t.Fatal("Flapping(period=0) error = nil, want an invalid-period error")
	}
}
