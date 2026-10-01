package controller

import (
	"errors"
	"strings"
	"testing"

	"github.com/raghavs6/KVFlow/internal/scheduler"
)

// Each kind of request prints as expected, and an error with commas and
// quotes stays in one field.
func TestWriteCSV(t *testing.T) {
	results := []Result{
		{Believed: scheduler.ActionTransfer, Action: scheduler.ActionTransfer, PredictedSeconds: 0.0003, Seconds: 0.00035},
		{Believed: scheduler.ActionRecompute, Action: scheduler.ActionTransfer, PredictedSeconds: 1048.576, Seconds: 0.0006},
		{Believed: scheduler.ActionTransfer, Action: scheduler.ActionTransfer, PredictedSeconds: 0.0003, Err: errors.New(`dial "peer": refused, sorry`)},
		{Believed: scheduler.ActionRecompute, Action: scheduler.ActionRecompute, PredictedSeconds: 1048.576},
	}
	var b strings.Builder
	if err := WriteCSV(&b, scenarios(len(results)), results); err != nil {
		t.Fatalf("WriteCSV() error = %v", err)
	}
	want := `index,bytes,believed,action,predicted,actual,error
1,1048576,transfer,transfer,0.000300,0.000350,
2,1048576,recompute,transfer,1048.576000,0.000600,
3,1048576,transfer,transfer,0.000300,,"dial ""peer"": refused, sorry"
4,1048576,recompute,recompute,1048.576000,,
`
	if got := b.String(); got != want {
		t.Errorf("WriteCSV() =\n%s\nwant\n%s", got, want)
	}
}
