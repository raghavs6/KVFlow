package main

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/raghavs6/KVFlow/internal/xfercsv"
)

// Transfers that follow an exact line (2 ms startup, 1 GB/s) must be fit
// back to that line, in kvxfer's CSV format and interleaved the same way.
func TestFitRecoversExactLine(t *testing.T) {
	const startup, spb = 0.002, 1e-9
	var csv strings.Builder
	csv.WriteString(xfercsv.Header + "\n")
	for round := 1; round <= 3; round++ {
		for _, n := range []float64{1 << 20, 16 << 20, 256 << 20} {
			fmt.Fprintf(&csv, "true,%d,%.0f,%.9f\n", round, n, startup+n*spb)
		}
	}
	rows, err := xfercsv.Parse(strings.NewReader(csv.String()))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
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
	for _, e := range errorsBySize(rows, gotStartup, gotSPB) {
		if math.Abs(e.percent()) > 0.01 {
			t.Errorf("%.0f bytes: error %.4f%%, want about 0", e.bytes, e.percent())
		}
	}
}

// Each size's measured time is the mean of its transfers, and the error is
// relative to that mean: a line predicting 1 s against transfers of 2 s and
// 4 s is 66.7% too fast.
func TestErrorsBySizeAveragesEachSize(t *testing.T) {
	rows := []xfercsv.Row{{Bytes: 1, Seconds: 2}, {Bytes: 5, Seconds: 5}, {Bytes: 1, Seconds: 4}}
	got := errorsBySize(rows, 0, 1)
	if len(got) != 2 || got[0].bytes != 1 || got[1].bytes != 5 {
		t.Fatalf("errorsBySize() sizes = %+v, want 1 then 5", got)
	}
	if got[0].measured != 3 || math.Abs(got[0].percent()+66.667) > 0.001 {
		t.Errorf("size 1: measured %g, error %.3f%%, want 3 and -66.667%%", got[0].measured, got[0].percent())
	}
	if got[1].percent() != 0 {
		t.Errorf("size 5: error %g%%, want 0", got[1].percent())
	}
}

// Dropping round 1 and splitting by connection mode keeps only the rows
// asked for.
func TestFilter(t *testing.T) {
	rows := []xfercsv.Row{
		{Reuse: true, Round: 1}, {Reuse: true, Round: 2},
		{Reuse: false, Round: 1}, {Reuse: false, Round: 2}, {Reuse: false, Round: 3},
	}
	if got := len(filter(rows, false, 2)); got != 2 {
		t.Errorf("filter(reuse=false, from round 2) kept %d rows, want 2", got)
	}
	if got := len(filter(rows, true, 1)); got != 2 {
		t.Errorf("filter(reuse=true, all rounds) kept %d rows, want 2", got)
	}
}
