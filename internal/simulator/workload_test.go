package simulator

import (
	"math"
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

func TestMixedPrefixes(t *testing.T) {
	base := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
	sizes := []int{2_000, 50_000}

	first, err := MixedPrefixes(rand.New(rand.NewPCG(1, 2)), 200, base, sizes)
	if err != nil {
		t.Fatalf("MixedPrefixes() error = %v", err)
	}
	second, err := MixedPrefixes(rand.New(rand.NewPCG(1, 2)), 200, base, sizes)
	if err != nil {
		t.Fatalf("MixedPrefixes() error = %v", err)
	}

	counts := map[int]int{}
	for i, s := range first {
		if s != second[i] {
			t.Fatalf("scenario %d differs between runs with the same seed", i)
		}
		counts[s.Request.PrefixTokens]++
		s.Request.PrefixTokens = base.Request.PrefixTokens
		if s != base {
			t.Fatalf("scenario %d changed more than the prefix size", i)
		}
	}
	if len(counts) != len(sizes) {
		t.Errorf("prefix size counts = %v, want every size of %v and nothing else", counts, sizes)
	}
	for _, size := range sizes {
		if counts[size] == 0 {
			t.Errorf("prefix size %d never drawn", size)
		}
	}
}

func TestMixedPrefixesRejectsNoSizes(t *testing.T) {
	base := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)

	if _, err := MixedPrefixes(rand.New(rand.NewPCG(1, 2)), 4, base, nil); err == nil {
		t.Fatal("MixedPrefixes(no sizes) error = nil, want an error")
	}
}

func TestDrift(t *testing.T) {
	fast := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
	slow := baseScenario(400*time.Millisecond, 50*time.Millisecond, 1_000_000_000)

	got := Drift(5, fast, slow)

	// Seconds per byte moves in equal steps from fast to slow and back.
	want := []float64{1e-10, 5.5e-10, 1e-9, 5.5e-10, 1e-10}
	if len(got) != len(want) {
		t.Fatalf("Drift() returned %d scenarios, want %d", len(got), len(want))
	}
	for i := range want {
		if spb := 1 / got[i].BandwidthBytesPerSec; math.Abs(spb-want[i]) > 1e-9*want[i] {
			t.Errorf("scenario %d seconds per byte = %g, want %g", i, spb, want[i])
		}
		s := got[i]
		s.BandwidthBytesPerSec = fast.BandwidthBytesPerSec
		if s != fast {
			t.Errorf("scenario %d changed more than bandwidth", i)
		}
	}
}

func TestDriftShortRuns(t *testing.T) {
	fast := baseScenario(400*time.Millisecond, 50*time.Millisecond, 10_000_000_000)
	slow := baseScenario(400*time.Millisecond, 50*time.Millisecond, 1_000_000_000)

	if got := Drift(0, fast, slow); len(got) != 0 {
		t.Errorf("Drift(0) = %d scenarios, want 0", len(got))
	}
	if got := Drift(1, fast, slow); len(got) != 1 || got[0] != fast {
		t.Errorf("Drift(1) = %+v, want [fast]", got)
	}
}
