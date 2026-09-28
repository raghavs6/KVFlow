// Command kvfit reads kvxfer's CSV on stdin and fits transfer time as
// startup + bytes * secondsPerByte, to check whether real transfers follow
// the line the learner assumes.
package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/raghavs6/KVFlow/internal/learner"
)

// fitAlpha is tiny so every transfer counts about equally, which makes
// learner.Line an ordinary least-squares fit. Over hundreds of rows the
// oldest point's weight differs from the newest by well under 0.1%.
const fitAlpha = 1e-6

const header = "reuse,round,bytes,seconds"

type row struct {
	reuse          bool
	round          int
	bytes, seconds float64
}

func main() {
	rows, err := parse(os.Stdin)
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
		}
	}
}

// parse reads kvxfer's CSV output.
func parse(r io.Reader) ([]row, error) {
	records, err := csv.NewReader(r).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("empty input, want header %q", header)
	}
	rows := make([]row, 0, len(records)-1)
	for i, rec := range records {
		if i == 0 {
			if strings.Join(rec, ",") != header {
				return nil, fmt.Errorf("header = %v, want %q", rec, header)
			}
			continue
		}
		var r row
		var errs [4]error
		r.reuse, errs[0] = strconv.ParseBool(rec[0])
		r.round, errs[1] = strconv.Atoi(rec[1])
		r.bytes, errs[2] = strconv.ParseFloat(rec[2], 64)
		r.seconds, errs[3] = strconv.ParseFloat(rec[3], 64)
		for _, err := range errs {
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", i+1, err)
			}
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// filter keeps rows in one connection mode from minRound on.
func filter(rows []row, reuse bool, minRound int) []row {
	var out []row
	for _, r := range rows {
		if r.reuse == reuse && r.round >= minRound {
			out = append(out, r)
		}
	}
	return out
}

// fit feeds rows, in order, to the same line learner the policies use.
func fit(rows []row) (time.Duration, float64, error) {
	l, err := learner.NewLine(fitAlpha, rows[0].seconds/rows[0].bytes)
	if err != nil {
		return 0, 0, err
	}
	for _, r := range rows {
		l.Observe(r.bytes, r.seconds)
	}
	return l.Startup(), l.SecondsPerByte(), nil
}
