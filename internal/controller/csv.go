package controller

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"

	"github.com/raghavs6/KVFlow/internal/scheduler"
	"github.com/raghavs6/KVFlow/internal/simulator"
)

// WriteCSV prints one row per request: its index from 1, seconds since the
// run started, prefix size, the
// believed and taken actions, predicted and measured transfer seconds, and
// why the transfer failed. actual is empty when nothing was measured, so it
// can't be read as a 0-second transfer. It uses encoding/csv because error
// text can hold commas and quotes.
func WriteCSV(w io.Writer, scenarios []simulator.Scenario, results []Result) error {
	cw := csv.NewWriter(w)
	cw.Write([]string{"index", "at", "bytes", "believed", "action", "predicted", "actual", "error"})
	for i, r := range results {
		actual, errText := "", ""
		if r.Err != nil {
			errText = r.Err.Error()
		} else if r.Action == scheduler.ActionTransfer {
			actual = fmt.Sprintf("%.6f", r.Seconds)
		}
		cw.Write([]string{
			strconv.Itoa(i + 1),
			fmt.Sprintf("%.6f", r.At.Seconds()),
			strconv.FormatInt(prefixBytes(scenarios[i]), 10),
			string(r.Believed),
			string(r.Action),
			fmt.Sprintf("%.6f", r.PredictedSeconds),
			actual,
			errText,
		})
	}
	cw.Flush()
	return cw.Error()
}
