package nettrace

import (
	"math"
	"testing"

	"github.com/raghavs6/KVFlow/internal/xfercsv"
)

// Two seconds at 100 B/s, then two at 10 B/s.
func stepTrace(t *testing.T) *Trace {
	t.Helper()
	tr, err := FromRows([]xfercsv.Row{
		{Bytes: 200, Seconds: 2},
		{Bytes: 20, Seconds: 2},
	})
	if err != nil {
		t.Fatalf("FromRows() error = %v", err)
	}
	return tr
}

func TestSendTime(t *testing.T) {
	tr := stepTrace(t)
	tests := []struct {
		name      string
		at, bytes float64
		want      float64
	}{
		{"inside the fast segment", 0, 100, 1},
		{"ends exactly at the step", 1, 100, 1},
		{"crosses the step", 1, 110, 2},
		{"starts on the step", 2, 10, 1},
		{"runs past the end at the last rate", 3, 30, 3},
		{"starts after the end", 10, 10, 1},
	}
	for _, tt := range tests {
		got := tr.SendTime(tt.at, tt.bytes)
		if math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("%s: SendTime(%v, %v) = %v, want %v", tt.name, tt.at, tt.bytes, got, tt.want)
		}
	}
}

func TestRateAtAndDuration(t *testing.T) {
	tr := stepTrace(t)
	if got := tr.Duration(); got != 4 {
		t.Errorf("Duration() = %v, want 4", got)
	}
	for _, c := range []struct{ at, want float64 }{{0, 100}, {1.99, 100}, {2, 10}, {9, 10}} {
		if got := tr.RateAt(c.at); got != c.want {
			t.Errorf("RateAt(%v) = %v, want %v", c.at, got, c.want)
		}
	}
}

func TestFromRowsRejectsBadRows(t *testing.T) {
	if _, err := FromRows(nil); err == nil {
		t.Error("FromRows(nil) error = nil, want an error")
	}
	if _, err := FromRows([]xfercsv.Row{{Bytes: 10, Seconds: 0}}); err == nil {
		t.Error("FromRows(zero seconds) error = nil, want an error")
	}
}
