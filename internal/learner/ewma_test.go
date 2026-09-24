package learner

import (
	"math"
	"testing"
)

func TestEWMAMovesAlphaOfTheWay(t *testing.T) {
	e, err := NewEWMA(0.5, 1e-10)
	if err != nil {
		t.Fatalf("NewEWMA() error = %v", err)
	}
	// A 2GB transfer taking 2s is 1e-9 s/B; half way from 1e-10 is 5.5e-10.
	e.Observe(2e9, 2)
	if got := e.SecondsPerByte(); math.Abs(got-5.5e-10) > 1e-20 {
		t.Errorf("SecondsPerByte() = %g, want 5.5e-10", got)
	}
	if got := e.Startup(); got != 0 {
		t.Errorf("Startup() = %v, want 0", got)
	}
}

func TestNewEWMARejectsInvalidInputs(t *testing.T) {
	for _, tt := range []struct{ alpha, secondsPerByte float64 }{
		{0, 1e-10}, {-0.5, 1e-10}, {1.5, 1e-10}, {0.5, 0}, {0.5, math.Inf(1)},
	} {
		if _, err := NewEWMA(tt.alpha, tt.secondsPerByte); err == nil {
			t.Errorf("NewEWMA(%v, %v) error = nil, want an error", tt.alpha, tt.secondsPerByte)
		}
	}
}
