package learner

import "testing"

func TestLastSampleKeepsOnlyTheLastTransfer(t *testing.T) {
	s, err := NewLastSample(initialSecondsPerByte)
	if err != nil {
		t.Fatalf("NewLastSample() error = %v", err)
	}
	if got := s.SecondsPerByte(); got != initialSecondsPerByte {
		t.Errorf("before any transfer, SecondsPerByte() = %g, want %g", got, initialSecondsPerByte)
	}
	s.Observe(100, 1)
	s.Observe(100, 4)
	if got := s.SecondsPerByte(); got != 0.04 {
		t.Errorf("SecondsPerByte() = %g, want 0.04", got)
	}
	if got := s.Startup(); got != 0 {
		t.Errorf("Startup() = %v, want 0", got)
	}
}

func TestNewLastSampleRejectsBadBelief(t *testing.T) {
	if _, err := NewLastSample(0); err == nil {
		t.Error("NewLastSample(0) error = nil, want an error")
	}
}
