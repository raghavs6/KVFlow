// Package nettrace turns a recorded run of back-to-back transfers into
// bandwidth over time, so a simulated load can be sent through the same
// network a real one saw.
package nettrace

import (
	"errors"
	"fmt"
	"sort"

	"github.com/raghavs6/KVFlow/internal/xfercsv"
)

// Trace is bandwidth as a step function of time. Segment i runs from
// ends[i-1] (0 for the first) to ends[i] at rates[i] bytes per second.
// After the last segment the last rate holds.
type Trace struct {
	ends  []float64
	rates []float64
}

// FromRows builds a trace from kvxfer rows sent back to back, so each
// transfer starts when the one before it ends and runs at its own average
// rate for its duration. Any per-transfer startup is spread over that
// transfer, which on a reused connection is too small to measure.
func FromRows(rows []xfercsv.Row) (*Trace, error) {
	if len(rows) == 0 {
		return nil, errors.New("no rows")
	}
	t := &Trace{ends: make([]float64, len(rows)), rates: make([]float64, len(rows))}
	end := 0.0
	for i, r := range rows {
		if !(r.Seconds > 0) || !(r.Bytes > 0) {
			return nil, fmt.Errorf("row %d: bytes and seconds must be positive", i+1)
		}
		end += r.Seconds
		t.ends[i] = end
		t.rates[i] = r.Bytes / r.Seconds
	}
	return t, nil
}

// Duration is when the recorded run ended, in seconds from its start.
func (t *Trace) Duration() float64 { return t.ends[len(t.ends)-1] }

// RateAt is the bandwidth, in bytes per second, at seconds into the trace.
func (t *Trace) RateAt(at float64) float64 { return t.rates[t.segment(at)] }

// SendTime is how many seconds sending bytes takes when it starts at
// seconds into the trace.
func (t *Trace) SendTime(at, bytes float64) float64 {
	now := at
	for i := t.segment(at); ; i++ {
		if i == len(t.ends)-1 {
			return now + bytes/t.rates[i] - at
		}
		fits := (t.ends[i] - now) * t.rates[i]
		if bytes <= fits {
			return now + bytes/t.rates[i] - at
		}
		bytes -= fits
		now = t.ends[i]
	}
}

// segment is the index of the segment that holds at. A time on a boundary
// belongs to the segment that starts there.
func (t *Trace) segment(at float64) int {
	i := sort.Search(len(t.ends), func(i int) bool { return t.ends[i] > at })
	return min(i, len(t.ends)-1)
}
