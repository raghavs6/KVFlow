package controller

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/raghavs6/KVFlow/internal/scheduler"
)

// Each kind of request prints as expected, and an error with commas and
// quotes stays in one field.
func TestWriteCSV(t *testing.T) {
	results := []Result{
		{Believed: scheduler.ActionTransfer, Action: scheduler.ActionTransfer, PredictedSeconds: 0.0003, Seconds: 0.00035},
		{At: 1500 * time.Microsecond, Believed: scheduler.ActionRecompute, Action: scheduler.ActionTransfer, PredictedSeconds: 1048.576, Seconds: 0.0006},
		{At: 2 * time.Second, Believed: scheduler.ActionTransfer, Action: scheduler.ActionTransfer, PredictedSeconds: 0.0003, Err: errors.New(`dial "peer": refused, sorry`)},
		{At: 2*time.Second + time.Nanosecond, Believed: scheduler.ActionRecompute, Action: scheduler.ActionRecompute, PredictedSeconds: 1048.576},
	}
	var b strings.Builder
	if err := WriteCSV(&b, scenarios(len(results)), results); err != nil {
		t.Fatalf("WriteCSV() error = %v", err)
	}
	want := `index,at,bytes,believed,action,predicted,actual,error
1,0.000000,1048576,transfer,transfer,0.000300,0.000350,
2,0.001500,1048576,recompute,transfer,1048.576000,0.000600,
3,2.000000,1048576,transfer,transfer,0.000300,,"dial ""peer"": refused, sorry"
4,2.000000,1048576,recompute,recompute,1048.576000,,
`
	if got := b.String(); got != want {
		t.Errorf("WriteCSV() =\n%s\nwant\n%s", got, want)
	}
}
