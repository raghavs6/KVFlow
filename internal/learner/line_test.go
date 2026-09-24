package learner

import (
	"math"
	"testing"
	"time"
)

// The learner starts believing 10 GB/s with no startup.
const initialSecondsPerByte = 1e-10

func newLine(t *testing.T, alpha float64) *Line {
	t.Helper()
	l, err := NewLine(alpha, initialSecondsPerByte)
	if err != nil {
		t.Fatalf("NewLine() error = %v", err)
	}
	return l
}

func assertEstimate(t *testing.T, l *Line, wantStartup time.Duration, wantSecondsPerByte float64) {
	t.Helper()
	if got := l.Startup(); absDuration(got-wantStartup) > time.Microsecond {
		t.Errorf("Startup() = %v, want %v", got, wantStartup)
	}
	if got := l.SecondsPerByte(); math.Abs(got-wantSecondsPerByte) > 1e-6*wantSecondsPerByte {
		t.Errorf("SecondsPerByte() = %g, want %g", got, wantSecondsPerByte)
	}
}

func TestLineStartsFromInitialBelief(t *testing.T) {
	assertEstimate(t, newLine(t, 0.1), 0, initialSecondsPerByte)
}

// Points on seconds = 0.2 + bytes/10GB/s recover both parameters.
func TestLineRecoversStartupAndBandwidth(t *testing.T) {
	l := newLine(t, 0.1)
	for range 10 {
		l.Observe(80e6, 0.208)
		l.Observe(400e6, 0.240)
		l.Observe(800e6, 0.280)
	}
	assertEstimate(t, l, 200*time.Millisecond, 1e-10)
}

// With one transfer size a line can't be fit, so the learner keeps its
// startup estimate (0) and passes the slope through the observed point.
func TestLineFallsBackWithOneSize(t *testing.T) {
	l := newLine(t, 0.1)
	for range 5 {
		l.Observe(2e9, 0.4)
	}
	assertEstimate(t, l, 0, 2e-10)
}

// A fitted startup is kept when later transfers all have one size, so the
// learner doesn't forget what it learned just because sizes stop varying.
func TestLineKeepsStartupWhenSizesStopVarying(t *testing.T) {
	l := newLine(t, 0.5)
	for range 10 {
		l.Observe(80e6, 0.208)
		l.Observe(800e6, 0.280)
	}
	// Same line, one size: the spread fades until no line can be fit.
	for range 60 {
		l.Observe(2e9, 0.4)
	}
	assertEstimate(t, l, 200*time.Millisecond, 1e-10)
}

func TestLineForgetsOldConditions(t *testing.T) {
	l := newLine(t, 0.5)
	for range 10 {
		l.Observe(80e6, 0.2+80e6/10e9)
		l.Observe(800e6, 0.2+800e6/10e9)
	}
	// The network slows to 1 GB/s; startup is unchanged.
	for range 30 {
		l.Observe(80e6, 0.2+80e6/1e9)
		l.Observe(800e6, 0.2+800e6/1e9)
	}
	assertEstimate(t, l, 200*time.Millisecond, 1e-9)
}

// These points fit a line with a -100ms startup; it is pinned at 0 and the
// slope is refit through the mean point.
func TestLineClampsNegativeStartup(t *testing.T) {
	l := newLine(t, 0.5)
	for range 20 {
		l.Observe(1e9, 0.05)
		l.Observe(2e9, 0.2)
	}
	if got := l.Startup(); got != 0 {
		t.Errorf("Startup() = %v, want 0", got)
	}
	if got := l.SecondsPerByte(); got <= 0 {
		t.Errorf("SecondsPerByte() = %g, want positive", got)
	}
}

// A line sloping down would mean bigger transfers are faster. The learner
// falls back instead of learning a negative bandwidth, and keeps the startup
// it had already learned rather than discarding it.
func TestLineRejectsNegativeSlope(t *testing.T) {
	l := newLine(t, 0.5)
	for range 10 {
		l.Observe(80e6, 0.208)
		l.Observe(800e6, 0.280)
	}
	for range 30 {
		l.Observe(1e9, 0.31)
		l.Observe(2e9, 0.29)
	}
	if got := l.SecondsPerByte(); got <= 0 {
		t.Errorf("SecondsPerByte() = %g, want positive", got)
	}
	if got := l.Startup(); got < 100*time.Millisecond {
		t.Errorf("Startup() = %v, want the learned startup kept", got)
	}
}

func TestNewLineRejectsInvalidInputs(t *testing.T) {
	for _, tt := range []struct{ alpha, secondsPerByte float64 }{
		{0, 1e-10}, {1.5, 1e-10}, {0.5, 0}, {0.5, -1}, {0.5, math.Inf(1)},
	} {
		if _, err := NewLine(tt.alpha, tt.secondsPerByte); err == nil {
			t.Errorf("NewLine(%v, %v) error = nil, want an error", tt.alpha, tt.secondsPerByte)
		}
	}
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// Sizes 1% apart with a 1ms timing wobble would fit a 300ms startup and a
// 20 GB/s network. That is noise, not evidence, so no line is fit.
func TestLineIgnoresTinySpread(t *testing.T) {
	l := newLine(t, 0.5)
	for range 20 {
		l.Observe(2.00e9, 0.400)
		l.Observe(2.02e9, 0.401)
	}
	if got := l.Startup(); got != 0 {
		t.Errorf("Startup() = %v, want 0", got)
	}
}
