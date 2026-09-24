// Package learner turns observed transfers into beliefs about the network.
package learner

import (
	"errors"
	"math"
	"time"
)

// minRelativeSpread is how much transfer sizes must vary, as a standard
// deviation relative to their mean, before a line is fit. Below it the points
// sit too close together to tell startup from per-byte cost. 10% is a guess,
// not a measured threshold.
const minRelativeSpread = 0.1

var (
	errInvalidAlpha          = errors.New("alpha must be in (0, 1]")
	errInvalidSecondsPerByte = errors.New("initial seconds per byte must be finite and positive")
)

// Line learns transfer time as startup + bytes * secondsPerByte by fitting a
// straight line through observed (bytes, seconds) points. Each new point
// fades the weight of older ones by (1 - alpha), as an EWMA does, so the line
// follows changing conditions.
type Line struct {
	alpha float64

	// Exponentially weighted sum of weights, means of bytes and seconds, and
	// sums of squared deviations. Keeping deviations from the mean, rather
	// than raw sums of bytes squared (~1e18), avoids losing precision when
	// subtracting two huge, nearly equal numbers.
	weight, meanBytes, meanSeconds float64
	bytesVar, bytesSecondsCov      float64

	startup        float64
	secondsPerByte float64
}

// NewLine returns a learner that believes transfers have no startup and cost
// initialSecondsPerByte until it observes otherwise.
func NewLine(alpha, initialSecondsPerByte float64) (*Line, error) {
	if !(alpha > 0 && alpha <= 1) {
		return nil, errInvalidAlpha
	}
	if !(initialSecondsPerByte > 0) || math.IsInf(initialSecondsPerByte, 0) {
		return nil, errInvalidSecondsPerByte
	}
	return &Line{alpha: alpha, secondsPerByte: initialSecondsPerByte}, nil
}

// Startup is the learned fixed delay per transfer.
func (l *Line) Startup() time.Duration {
	return time.Duration(l.startup * float64(time.Second))
}

// SecondsPerByte is the learned slope, the inverse of bandwidth.
func (l *Line) SecondsPerByte() float64 {
	return l.secondsPerByte
}

// Observe adds one transfer of bytes (> 0) that took seconds and refits.
func (l *Line) Observe(bytes, seconds float64) {
	// Fade old points, then add this one with weight 1 (weighted Welford).
	decay := 1 - l.alpha
	l.weight = decay*l.weight + 1
	dBytes := bytes - l.meanBytes
	l.meanBytes += dBytes / l.weight
	l.meanSeconds += (seconds - l.meanSeconds) / l.weight
	l.bytesVar = decay*l.bytesVar + dBytes*(bytes-l.meanBytes)
	l.bytesSecondsCov = decay*l.bytesSecondsCov + dBytes*(seconds-l.meanSeconds)

	spread := math.Sqrt(l.bytesVar/l.weight) / l.meanBytes
	if spread >= minRelativeSpread {
		slope := l.bytesSecondsCov / l.bytesVar
		startup := l.meanSeconds - slope*l.meanBytes
		switch {
		case slope > 0 && startup >= 0:
			l.startup, l.secondsPerByte = startup, slope
			return
		case slope > 0:
			// A negative startup is impossible; pin it at 0.
			l.startup, l.secondsPerByte = 0, l.meanSeconds/l.meanBytes
			return
		}
		// A falling line would mean negative bandwidth; fall back.
	}

	// Too little spread to fit a line: keep the startup estimate and pass
	// the slope through the mean point. A startup longer than the mean
	// transfer can't be right, so drop it.
	if l.startup >= l.meanSeconds {
		l.startup = 0
	}
	l.secondsPerByte = (l.meanSeconds - l.startup) / l.meanBytes
}
