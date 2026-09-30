package main

import (
	"bytes"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raghavs6/KVFlow/internal/xfer"
)

// countingListener counts accepted connections.
type countingListener struct {
	net.Listener
	accepted atomic.Int32
}

func (l *countingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.accepted.Add(1)
	}
	return c, err
}

// run writes a header, then one row per transfer with sizes interleaved
// within numbered rounds, in both connection modes. Reuse dials once;
// otherwise every transfer dials its own connection.
func TestRunInterleavesSizes(t *testing.T) {
	for _, reuse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reuse=%t", reuse), func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("Listen() error = %v", err)
			}
			defer ln.Close()
			counted := &countingListener{Listener: ln}
			go xfer.ServeAll(counted, t.Logf)

			var out bytes.Buffer
			if err := run(&out, ln.Addr().String(), reuse, []int64{10, 20}, 2, 0); err != nil {
				t.Fatalf("run() error = %v", err)
			}
			lines := strings.Split(strings.TrimSpace(out.String()), "\n")
			want := []string{"reuse,round,bytes,seconds", "1,10", "1,20", "2,10", "2,20"}
			if len(lines) != len(want) {
				t.Fatalf("run() wrote %d lines, want %d:\n%s", len(lines), len(want), out.String())
			}
			for i, line := range lines[1:] {
				fields := strings.Split(line, ",")
				if fields[0] != fmt.Sprint(reuse) || fields[1]+","+fields[2] != want[i+1] {
					t.Errorf("row %d = %q, want reuse=%t round,bytes=%s", i, line, reuse, want[i+1])
				}
				if s, err := strconv.ParseFloat(fields[3], 64); err != nil || s <= 0 {
					t.Errorf("row %d seconds = %q, want > 0", i, fields[3])
				}
			}
			wantConns := int32(len(lines) - 1)
			if reuse {
				wantConns = 1
			}
			if got := counted.accepted.Load(); got != wantConns {
				t.Errorf("receiver accepted %d connections, want %d", got, wantConns)
			}
		})
	}
}

// TestRunDurationFinishesTheRound checks that with a duration, run only
// stops between rounds: a 1 ns duration is over before the first round
// ends, so exactly one full round is sent, whatever repeats says.
func TestRunDurationFinishesTheRound(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()
	go xfer.ServeAll(ln, t.Logf)

	var out bytes.Buffer
	if err := run(&out, ln.Addr().String(), true, []int64{10, 20, 30}, 5, time.Nanosecond); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	want := []string{"1,10", "1,20", "1,30"}
	if len(lines) != len(want)+1 {
		t.Fatalf("run() wrote %d lines, want %d:\n%s", len(lines), len(want)+1, out.String())
	}
	for i, line := range lines[1:] {
		fields := strings.Split(line, ",")
		if got := fields[1] + "," + fields[2]; got != want[i] {
			t.Errorf("row %d round,bytes = %s, want %s", i, got, want[i])
		}
	}
}
