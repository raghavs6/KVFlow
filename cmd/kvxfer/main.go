// Command kvxfer times real TCP transfers so the transfer learner can be
// checked against a real network.
package main

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
)

// serve handles transfers on conn until the sender closes it. A transfer is
// an 8-byte big-endian length, then that many bytes. After reading them all,
// serve replies with one byte so the sender knows they arrived.
func serve(conn net.Conn) error {
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
