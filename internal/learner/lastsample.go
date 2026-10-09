package learner

import (
	"math"
	"time"
)

// LastSample believes the last transfer's rate will hold for the next one.
// It is CacheGen's estimator: it "estimates the bandwidth by measuring the
// throughput of the previous chunk" and "assumes this throughput will remain
// constant" (Liu et al., SIGCOMM '24, §5.3). Like EWMA it has no startup
// term; it is EWMA with alpha 1.
type LastSample struct {
	secondsPerByte float64
}

// NewLastSample returns a learner that believes transfers cost
// initialSecondsPerByte until it observes one.
func NewLastSample(initialSecondsPerByte float64) (*LastSample, error) {
	if !(initialSecondsPerByte > 0) || math.IsInf(initialSecondsPerByte, 0) {
		return nil, errInvalidSecondsPerByte
	}
	return &LastSample{secondsPerByte: initialSecondsPerByte}, nil
}

// Observe replaces the belief with this transfer's rate.
func (s *LastSample) Observe(bytes, seconds float64) { s.secondsPerByte = seconds / bytes }

// Startup is always 0: this learner has no startup term.
func (s *LastSample) Startup() time.Duration { return 0 }

// SecondsPerByte is the last transfer's rate, inverted.
func (s *LastSample) SecondsPerByte() float64 { return s.secondsPerByte }
