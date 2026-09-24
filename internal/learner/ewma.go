package learner

import (
	"math"
	"time"
)

// EWMA learns only seconds per byte, as an exponentially weighted moving
// average of each transfer's duration divided by its size. It has no startup
// term, so a fixed per-transfer cost is folded into the per-byte estimate.
type EWMA struct {
	alpha float64
	// Averaging seconds per byte rather than bytes per second keeps transfer
	// time linear in the estimate, so the averaged estimate matches the
	// average observed time.
	secondsPerByte float64
}

// NewEWMA returns a learner that believes transfers cost
// initialSecondsPerByte until it observes otherwise.
func NewEWMA(alpha, initialSecondsPerByte float64) (*EWMA, error) {
	if !(alpha > 0 && alpha <= 1) {
		return nil, errInvalidAlpha
	}
	if !(initialSecondsPerByte > 0) || math.IsInf(initialSecondsPerByte, 0) {
		return nil, errInvalidSecondsPerByte
	}
	return &EWMA{alpha: alpha, secondsPerByte: initialSecondsPerByte}, nil
}

// Observe adds one transfer of bytes (> 0) that took seconds.
func (e *EWMA) Observe(bytes, seconds float64) {
	e.secondsPerByte = (1-e.alpha)*e.secondsPerByte + e.alpha*seconds/bytes
}

// Startup is always 0: this learner has no startup term.
func (e *EWMA) Startup() time.Duration { return 0 }

// SecondsPerByte is the learned average, the inverse of bandwidth.
func (e *EWMA) SecondsPerByte() float64 { return e.secondsPerByte }
