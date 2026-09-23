package simulator

import (
	"math/rand/v2"
	"testing"
	"time"
)

func TestWorkloads(t *testing.T) {
	fast := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
	slow := baseScenario(400*time.Millisecond, 50*time.Millisecond, 1_000_000_000)
	F, S := fast.BandwidthBytesPerSec, slow.BandwidthBytesPerSec

	// Equal min and max periods make the phases fixed.
	flapping, err := Flapping(rand.New(rand.NewPCG(1, 2)), 7, 2, 2, fast, slow)
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

func TestFlappingRandomPhases(t *testing.T) {
	fast := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
	slow := baseScenario(400*time.Millisecond, 50*time.Millisecond, 1_000_000_000)
	const minPeriod, maxPeriod = 3, 7

	first, err := Flapping(rand.New(rand.NewPCG(1, 2)), 500, minPeriod, maxPeriod, fast, slow)
	if err != nil {
		t.Fatalf("Flapping() error = %v", err)
	}
	second, err := Flapping(rand.New(rand.NewPCG(1, 2)), 500, minPeriod, maxPeriod, fast, slow)
	if err != nil {
		t.Fatalf("Flapping() error = %v", err)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("scenario %d differs between runs with the same seed", i)
		}
	}

	if first[0] != fast {
		t.Fatal("first phase is slow, want fast")
	}
	// Split into phases of equal scenarios. Every phase but the last, which
	// the end of the run may cut short, must be within the bounds, and
	// neighbours differ by construction.
	var lengths []int
	run := 1
	for i := 1; i < len(first); i++ {
		if first[i] == first[i-1] {
			run++
			continue
		}
		lengths = append(lengths, run)
		run = 1
	}
	seen := map[int]bool{}
	for i, length := range lengths {
		if length < minPeriod || length > maxPeriod {
			t.Errorf("phase %d length = %d, want in [%d, %d]", i, length, minPeriod, maxPeriod)
		}
		seen[length] = true
	}
	if len(seen) < 2 {
		t.Errorf("phase lengths = %v, want them to vary", lengths)
	}
}

func TestFlappingRejectsInvalidPeriods(t *testing.T) {
	fast := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)

	for _, tt := range []struct{ min, max int }{{0, 5}, {-1, 5}, {5, 4}} {
		if _, err := Flapping(rand.New(rand.NewPCG(1, 2)), 4, tt.min, tt.max, fast, fast); err == nil {
			t.Errorf("Flapping(min=%d, max=%d) error = nil, want an invalid-period error", tt.min, tt.max)
		}
	}
}
