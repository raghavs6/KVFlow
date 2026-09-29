package main

import (
	"math"
	"testing"

	"github.com/raghavs6/KVFlow/internal/learner"
	"github.com/raghavs6/KVFlow/internal/simulator"
	"github.com/raghavs6/KVFlow/internal/xfercsv"
)

// The first prediction must come from the starting belief, not from the
// transfer being predicted: 1 GB at the starting 10 GB/s is 0.1 s, even
// though the transfer really took 5 s.
func TestReplayPredictsBeforeLearning(t *testing.T) {
	l, err := learner.NewLine(0.5, initialSecondsPerByte)
	if err != nil {
		t.Fatal(err)
	}
	rows := []xfercsv.Row{{Bytes: 1e9, Seconds: 5}, {Bytes: 1e9, Seconds: 5}}
	got := replay(rows, []simulator.Learner{l})
	if math.Abs(got[0][0]-0.1) > 1e-12 {
		t.Errorf("first prediction = %g s, want 0.1 s from the starting belief", got[0][0])
	}
	if math.Abs(got[1][0]-5) > 1e-9 {
		t.Errorf("second prediction = %g s, want 5 s after seeing one 5 s transfer", got[1][0])
	}
}

// Transfers on an exact line (1 ms startup, 1 GB/s), interleaved as kvxfer
// sends them, must be predicted almost exactly once the learner has seen a
// few rounds.
func TestReplayLearnsAnExactLine(t *testing.T) {
	const startup, spb = 0.001, 1e-9
	var rows []xfercsv.Row
	for round := 1; round <= 10; round++ {
		for _, n := range []float64{1 << 20, 4 << 20, 16 << 20, 64 << 20, 256 << 20} {
			rows = append(rows, xfercsv.Row{Reuse: true, Round: round, Bytes: n, Seconds: startup + n*spb})
		}
	}
	l, err := learner.NewLine(0.5, initialSecondsPerByte)
	if err != nil {
		t.Fatal(err)
	}
	got := replay(rows, []simulator.Learner{l})
	for i, r := range rows[len(rows)-5:] {
		p := got[len(rows)-5+i][0]
		if math.Abs(p-r.Seconds)/r.Seconds > 1e-3 {
			t.Errorf("%.0f bytes: predicted %g s, want %g s", r.Bytes, p, r.Seconds)
		}
	}
}
