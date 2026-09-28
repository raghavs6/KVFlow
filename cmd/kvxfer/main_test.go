package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// startServe accepts one connection on a localhost port, serves it, and
// sends serve's result on the returned channel.
func startServe(t *testing.T) (string, <-chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		done <- serve(conn)
	}()
	return ln.Addr().String(), done
}

// sendRaw writes a header announcing announced bytes, then sent bytes.
func sendRaw(t *testing.T, conn net.Conn, announced, sent int) {
	t.Helper()
	var header [8]byte
	binary.BigEndian.PutUint64(header[:], uint64(announced))
	if _, err := conn.Write(header[:]); err != nil {
		t.Fatalf("Write(header) error = %v", err)
	}
	if _, err := conn.Write(make([]byte, sent)); err != nil {
		t.Fatalf("Write(body) error = %v", err)
	}
}

// Two transfers on one connection each get a reply, and closing the
// connection afterwards ends serve without error.
func TestServeRepliesToEachTransfer(t *testing.T) {
	addr, done := startServe(t)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	for _, n := range []int{10, 3 << 20} {
		sendRaw(t, conn, n, n)
		var ack [1]byte
		if _, err := io.ReadFull(conn, ack[:]); err != nil {
			t.Fatalf("read reply after %d bytes: %v", n, err)
		}
	}
	conn.Close()
	if err := <-done; err != nil {
		t.Errorf("serve() error = %v, want nil", err)
	}
}

// serve must read every announced byte, so a sender that stops short is an
// error rather than a completed transfer.
func TestServeRejectsShortTransfer(t *testing.T) {
	addr, done := startServe(t)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	sendRaw(t, conn, 100, 99)
	conn.Close()
	if err := <-done; err == nil {
		t.Error("serve() error = nil, want an error for a short transfer")
	}
}

// transfer and serve agree on the format: sizes that are not a multiple of
// the write chunk still arrive exactly, or serve would misread the next
// header and fail.
func TestTransferRoundTrip(t *testing.T) {
	addr, done := startServe(t)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	for _, n := range []int64{1, int64(len(chunk)), int64(len(chunk)) + 1, 5 << 20} {
		if err := transfer(conn, n); err != nil {
			t.Fatalf("transfer(%d) error = %v", n, err)
		}
	}
	conn.Close()
	if err := <-done; err != nil {
		t.Errorf("serve() error = %v, want nil", err)
	}
}

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
			go serveAll(counted, t.Logf)

			var out bytes.Buffer
			if err := run(&out, ln.Addr().String(), reuse, []int64{10, 20}, 2); err != nil {
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
