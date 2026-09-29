// Command kvreplay reads kvxfer's CSV on stdin and feeds the transfers, in
// order, to the learners the policies use. Before each transfer, every
// learner predicts how long it will take; only then does it see the real
// time. It writes one CSV row per transfer with each learner's prediction,
// to show how the online learners track a real network over time.
package main

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/raghavs6/KVFlow/internal/learner"
	"github.com/raghavs6/KVFlow/internal/simulator"
	"github.com/raghavs6/KVFlow/internal/xfercsv"
)

// initialSecondsPerByte is kvbench's starting belief, a 10 GB/s network, so
// the replay tests the learners as kvbench configures them.
const initialSecondsPerByte = 1.0 / 10e9

// alphas are the ones kvbench compares.
var alphas = []float64{0.1, 0.5}

func main() {
	rows, err := xfercsv.Parse(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}
	var names []string
	var learners []simulator.Learner
	for _, kind := range []string{"line", "ewma"} {
		for _, alpha := range alphas {
			var l simulator.Learner
			var err error
			if kind == "line" {
				l, err = learner.NewLine(alpha, initialSecondsPerByte)
			} else {
				l, err = learner.NewEWMA(alpha, initialSecondsPerByte)
			}
			if err != nil {
				log.Fatal(err)
			}
			names = append(names, fmt.Sprintf("%s_%.1f", kind, alpha))
			learners = append(learners, l)
		}
	}
	write(os.Stdout, rows, names, replay(rows, learners))
}

// replay returns, for each row in order, every learner's predicted seconds
// for that transfer, made before the learner observes it.
func replay(rows []xfercsv.Row, learners []simulator.Learner) [][]float64 {
	predictions := make([][]float64, len(rows))
	for i, r := range rows {
		predictions[i] = make([]float64, len(learners))
		for j, l := range learners {
			predictions[i][j] = l.Startup().Seconds() + r.Bytes*l.SecondsPerByte()
			l.Observe(r.Bytes, r.Seconds)
		}
	}
	return predictions
}

// write prints one row per transfer: its index from 1, size, real seconds,
// and each learner's prediction in seconds.
func write(w io.Writer, rows []xfercsv.Row, names []string, predictions [][]float64) {
	fmt.Fprint(w, "index,bytes,actual")
	for _, n := range names {
		fmt.Fprint(w, ",", n)
	}
	fmt.Fprintln(w)
	for i, r := range rows {
		fmt.Fprintf(w, "%d,%.0f,%.6f", i+1, r.Bytes, r.Seconds)
		for _, p := range predictions[i] {
			fmt.Fprintf(w, ",%.6f", p)
		}
		fmt.Fprintln(w)
	}
}
