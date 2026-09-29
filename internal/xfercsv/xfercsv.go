// Package xfercsv reads the CSV that kvxfer writes, one row per timed
// transfer, so every tool that analyzes transfers agrees on the format.
package xfercsv

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Header is the first line of kvxfer's output.
const Header = "reuse,round,bytes,seconds"

// Row is one timed transfer.
type Row struct {
	Reuse          bool
	Round          int
	Bytes, Seconds float64
}

// Parse reads kvxfer's CSV output.
func Parse(r io.Reader) ([]Row, error) {
	records, err := csv.NewReader(r).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("empty input, want header %q", Header)
	}
	rows := make([]Row, 0, len(records)-1)
	for i, rec := range records {
		if i == 0 {
			if strings.Join(rec, ",") != Header {
				return nil, fmt.Errorf("header = %v, want %q", rec, Header)
			}
			continue
		}
		var r Row
		var errs [4]error
		r.Reuse, errs[0] = strconv.ParseBool(rec[0])
		r.Round, errs[1] = strconv.Atoi(rec[1])
		r.Bytes, errs[2] = strconv.ParseFloat(rec[2], 64)
		r.Seconds, errs[3] = strconv.ParseFloat(rec[3], 64)
		for _, err := range errs {
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", i+1, err)
			}
		}
		rows = append(rows, r)
	}
	return rows, nil
}
