// Command kvxfer times real TCP transfers so the transfer learner can be
// checked against a real network.
package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"
)

const repeats = 5

var sizes = []int64{1 << 20, 4 << 20, 16 << 20, 64 << 20, 256 << 20}

// Usage:
//
//	kvxfer recv -addr :9000
//	kvxfer send -addr localhost:9000 [-reuse] [-duration 20m]
func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: kvxfer recv|send [flags]")
	}
	fs := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	addr := fs.String("addr", "localhost:9000", "address to listen on or send to")
	reuse := fs.Bool("reuse", false, "send: keep one connection open for every transfer")
	duration := fs.Duration("duration", 0, "send: keep sending rounds until this much time has passed, instead of a fixed number")
	fs.Parse(os.Args[2:])

	switch os.Args[1] {
	case "recv":
		ln, err := net.Listen("tcp", *addr)
		if err != nil {
			log.Fatal(err)
		}
		log.Fatal(serveAll(ln, log.Printf))
	case "send":
		if err := run(os.Stdout, *addr, *reuse, sizes, repeats, *duration); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unknown mode %q: want recv or send", os.Args[1])
	}
}

// run times repeats transfers of every size to addr and writes one CSV row
// per transfer, numbering rounds from 1 so warm-up rows can be told apart. Sizes are interleaved so a slow moment on the machine hits
// every size a little instead of one size a lot. Without reuse, each
// transfer dials a new connection inside the timed span, so it pays the
// handshake and a fresh TCP ramp-up; with reuse, one connection is dialed
// before timing starts. A duration > 0 replaces repeats: rounds continue
// until that much time has passed, checked only between rounds so every
// round has every size.
func run(w io.Writer, addr string, reuse bool, sizes []int64, repeats int, duration time.Duration) error {
	var shared net.Conn
	if reuse {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			return err
		}
		defer c.Close()
		shared = c
	}
	fmt.Fprintln(w, "reuse,round,bytes,seconds")
	begin := time.Now()
	for round := 1; ; round++ {
		for _, n := range sizes {
			start := time.Now()
			conn := shared
			if !reuse {
				c, err := net.Dial("tcp", addr)
				if err != nil {
					return err
				}
				conn = c
			}
			err := transfer(conn, n)
			elapsed := time.Since(start)
			if !reuse {
				conn.Close()
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "%t,%d,%d,%.6f\n", reuse, round, n, elapsed.Seconds())
		}
		done := round >= repeats
		if duration > 0 {
			done = time.Since(begin) >= duration
		}
		if done {
			return nil
		}
	}
}

// chunk is reused for every write, so sending n bytes needs only 1 MB of
// memory. TCP does not compress, so zeros cost the same as real KV data.
var chunk = make([]byte, 1<<20)

// transfer sends n zero bytes over conn and returns once the receiver
// confirms it read them all.
func transfer(conn net.Conn, n int64) error {
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

// serveAll serves every connection on ln, each in its own goroutine, until
// ln is closed. A failed transfer only ends its own connection.
func serveAll(ln net.Listener, logf func(format string, args ...any)) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go func() {
			if err := serve(conn); err != nil {
				logf("serve %s: %v", conn.RemoteAddr(), err)
			}
		}()
	}
}

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
