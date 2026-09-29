// Command kvfit reads kvxfer's CSV on stdin and fits transfer time as
// startup + bytes * secondsPerByte, to check whether real transfers follow
// the line the learner assumes.
package main

import (
	"fmt"
	"log"
	"os"
	"text/tabwriter"
	"time"

	"github.com/raghavs6/KVFlow/internal/learner"
	"github.com/raghavs6/KVFlow/internal/xfercsv"
)

// fitAlpha is tiny so every transfer counts about equally, which makes
// learner.Line an ordinary least-squares fit. Over hundreds of rows the
// oldest point's weight differs from the newest by well under 0.1%.
const fitAlpha = 1e-6

func main() {
	rows, err := xfercsv.Parse(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}
	for _, reuse := range []bool{false, true} {
		for _, minRound := range []int{1, 2} {
			sel := filter(rows, reuse, minRound)
			if len(sel) == 0 {
				continue
			}
			startup, spb, err := fit(sel)
			if err != nil {
				log.Fatal(err)
			}
			label := "all rounds"
			if minRound > 1 {
				label = "without round 1"
			}
			fmt.Printf("reuse=%t, %s (%d transfers): startup %.3f ms, bandwidth %.2f GB/s\n",
				reuse, label, len(sel), startup.Seconds()*1000, 1/spb/1e9)
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
			fmt.Fprintln(tw, "size\tmeasured\tpredicted\terror\t")
			for _, e := range errorsBySize(sel, startup, spb) {
				fmt.Fprintf(tw, "%.0f MiB\t%.3f ms\t%.3f ms\t%+.1f%%\t\n",
					e.bytes/(1<<20), e.measured*1000, e.predicted*1000, e.percent())
			}
			tw.Flush()
			fmt.Println()
		}
	}
}

// sizeError compares one transfer size's mean measured time to the line.
type sizeError struct {
	bytes, measured, predicted float64
}

// percent is positive when the line predicts a slower transfer than was
// measured.
func (e sizeError) percent() float64 {
	return 100 * (e.predicted - e.measured) / e.measured
}

// errorsBySize reports each size in the order it first appears. Errors are
// per size, not one overall score, because least squares minimizes misses
// in seconds and so favors the largest transfers.
func errorsBySize(rows []xfercsv.Row, startup time.Duration, secondsPerByte float64) []sizeError {
	var order []float64
	sum := map[float64]float64{}
	count := map[float64]int{}
	for _, r := range rows {
		if count[r.Bytes] == 0 {
			order = append(order, r.Bytes)
		}
		sum[r.Bytes] += r.Seconds
		count[r.Bytes]++
	}
	out := make([]sizeError, len(order))
	for i, b := range order {
		out[i] = sizeError{
			bytes:     b,
			measured:  sum[b] / float64(count[b]),
			predicted: startup.Seconds() + b*secondsPerByte,
		}
	}
	return out
}

// filter keeps rows in one connection mode from minRound on.
func filter(rows []xfercsv.Row, reuse bool, minRound int) []xfercsv.Row {
	var out []xfercsv.Row
	for _, r := range rows {
		if r.Reuse == reuse && r.Round >= minRound {
			out = append(out, r)
		}
	}
	return out
}

// fit feeds rows, in order, to the same line learner the policies use.
func fit(rows []xfercsv.Row) (time.Duration, float64, error) {
	l, err := learner.NewLine(fitAlpha, rows[0].Seconds/rows[0].Bytes)
	if err != nil {
		return 0, 0, err
	}
	for _, r := range rows {
		l.Observe(r.Bytes, r.Seconds)
	}
	return l.Startup(), l.SecondsPerByte(), nil
}
