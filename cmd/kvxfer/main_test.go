package main

import (
	"encoding/binary"
	"io"
	"net"
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
