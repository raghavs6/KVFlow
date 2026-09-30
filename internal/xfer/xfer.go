// Package xfer is the wire format for real KV transfers: an 8-byte
// big-endian length, that many bytes, then a 1-byte reply from the receiver
// once it has read them all.
package xfer

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
)

// chunk is reused for every write, so sending n bytes needs only 1 MB of
// memory. TCP does not compress, so zeros cost the same as real KV data.
var chunk = make([]byte, 1<<20)

// Send sends n zero bytes over conn and returns once the receiver
// confirms it read them all.
func Send(conn net.Conn, n int64) error {
	var header [8]byte
	binary.BigEndian.PutUint64(header[:], uint64(n))
	if _, err := conn.Write(header[:]); err != nil {
		return err
	}
	for left := n; left > 0; {
		k := min(left, int64(len(chunk)))
		if _, err := conn.Write(chunk[:k]); err != nil {
			return err
		}
		left -= k
	}
	var ack [1]byte
	_, err := io.ReadFull(conn, ack[:])
	return err
}

// ServeAll serves every connection on ln, each in its own goroutine, until
// ln is closed. A failed transfer only ends its own connection.
func ServeAll(ln net.Listener, logf func(format string, args ...any)) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go func() {
			if err := Serve(conn); err != nil {
				logf("serve %s: %v", conn.RemoteAddr(), err)
			}
		}()
	}
}

// Serve handles transfers on conn until the sender closes it. A transfer is
// an 8-byte big-endian length, then that many bytes. After reading them all,
// Serve replies with one byte so the sender knows they arrived.
func Serve(conn net.Conn) error {
	defer conn.Close()
	var header [8]byte
	for {
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			// EOF before any header byte is the sender closing cleanly.
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		n := int64(binary.BigEndian.Uint64(header[:]))
		if _, err := io.CopyN(io.Discard, conn, n); err != nil {
			return err
		}
		if _, err := conn.Write([]byte{1}); err != nil {
			return err
		}
	}
}
