package xfercsv

import (
	"strings"
	"testing"
)

func TestParseReadsRows(t *testing.T) {
	got, err := Parse(strings.NewReader(Header + "\ntrue,2,1048576,0.000850\nfalse,1,4,0.5\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := []Row{{Reuse: true, Round: 2, Bytes: 1048576, Seconds: 0.00085}, {Round: 1, Bytes: 4, Seconds: 0.5}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Parse() = %+v, want %+v", got, want)
	}
}

func TestParseRejectsWrongHeader(t *testing.T) {
	if _, err := Parse(strings.NewReader("reuse,bytes,seconds\ntrue,1,0.1\n")); err == nil {
		t.Error("Parse() error = nil, want an error for the old header without round")
	}
}
