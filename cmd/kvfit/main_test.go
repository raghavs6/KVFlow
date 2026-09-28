package main

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// Transfers that follow an exact line (2 ms startup, 1 GB/s) must be fit
// back to that line, in kvxfer's CSV format and interleaved the same way.
func TestFitRecoversExactLine(t *testing.T) {
	const startup, spb = 0.002, 1e-9
	var csv strings.Builder
	csv.WriteString(header + "\n")
	for round := 1; round <= 3; round++ {
		for _, n := range []float64{1 << 20, 16 << 20, 256 << 20} {
			fmt.Fprintf(&csv, "true,%d,%.0f,%.9f\n", round, n, startup+n*spb)
		}
	}
	rows, err := parse(strings.NewReader(csv.String()))
	if err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	gotStartup, gotSPB, err := fit(filter(rows, true, 1))
	if err != nil {
		t.Fatalf("fit() error = %v", err)
	}
	if d := gotStartup - 2*time.Millisecond; d < -time.Microsecond || d > time.Microsecond {
		t.Errorf("startup = %v, want 2ms", gotStartup)
	}
	if math.Abs(gotSPB-spb)/spb > 1e-6 {
		t.Errorf("secondsPerByte = %g, want %g", gotSPB, spb)
	}
}

// Dropping round 1 and splitting by connection mode keeps only the rows
// asked for.
func TestFilter(t *testing.T) {
	rows := []row{
		{reuse: true, round: 1}, {reuse: true, round: 2},
		{reuse: false, round: 1}, {reuse: false, round: 2}, {reuse: false, round: 3},
	}
	if got := len(filter(rows, false, 2)); got != 2 {
		t.Errorf("filter(reuse=false, from round 2) kept %d rows, want 2", got)
	}
	if got := len(filter(rows, true, 1)); got != 2 {
		t.Errorf("filter(reuse=true, all rounds) kept %d rows, want 2", got)
	}
}

func TestParseRejectsWrongHeader(t *testing.T) {
	if _, err := parse(strings.NewReader("reuse,bytes,seconds\ntrue,1,0.1\n")); err == nil {
		t.Error("parse() error = nil, want an error for the old header without round")
	}
}
