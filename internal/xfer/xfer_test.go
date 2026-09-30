package xfer

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
)

// startServe accepts one connection on a localhost port, serves it, and
// sends Serve's result on the returned channel.
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
		done <- Serve(conn)
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
// connection afterwards ends Serve without error.
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
		t.Errorf("Serve() error = %v, want nil", err)
	}
}

// Serve must read every announced byte, so a sender that stops short is an
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
		t.Error("Serve() error = nil, want an error for a short transfer")
	}
}

// Send and Serve agree on the format: sizes that are not a multiple of
// the write chunk still arrive exactly, or Serve would misread the next
// header and fail.
func TestSendRoundTrip(t *testing.T) {
	addr, done := startServe(t)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	for _, n := range []int64{1, int64(len(chunk)), int64(len(chunk)) + 1, 5 << 20} {
		if err := Send(conn, n); err != nil {
			t.Fatalf("Send(%d) error = %v", n, err)
		}
	}
	conn.Close()
	if err := <-done; err != nil {
		t.Errorf("Serve() error = %v, want nil", err)
	}
}
