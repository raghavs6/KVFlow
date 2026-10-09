// Command kvxfer times real TCP transfers so the transfer learner can be
// checked against a real network.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	"github.com/raghavs6/KVFlow/internal/xfer"
	"github.com/raghavs6/KVFlow/internal/xfercsv"
)

const repeats = 5

var sizes = []int64{1 << 20, 4 << 20, 16 << 20, 64 << 20, 256 << 20}

// Usage:
//
//	kvxfer recv -addr :9000
//	kvxfer send -addr localhost:9000 [-reuse] [-duration 20m] [-gap 100ms]
func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: kvxfer recv|send [flags]")
	}
	fs := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	addr := fs.String("addr", "localhost:9000", "address to listen on or send to")
	reuse := fs.Bool("reuse", false, "send: keep one connection open for every transfer")
	duration := fs.Duration("duration", 0, "send: keep sending rounds until this much time has passed, instead of a fixed number")
	gap := fs.Duration("gap", 0, "send: leave the connection idle this long before each transfer after the first")
	fs.Parse(os.Args[2:])

	switch os.Args[1] {
	case "recv":
		ln, err := net.Listen("tcp", *addr)
		if err != nil {
			log.Fatal(err)
		}
		log.Fatal(xfer.ServeAll(ln, log.Printf))
	case "send":
		if err := run(os.Stdout, *addr, *reuse, sizes, repeats, *duration, *gap); err != nil {
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
// round has every size. A gap > 0 idles the link, untimed, before every
// transfer but the first, to measure what an idle connection costs.
func run(w io.Writer, addr string, reuse bool, sizes []int64, repeats int, duration, gap time.Duration) error {
	var shared net.Conn
	if reuse {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			return err
		}
		defer c.Close()
		shared = c
	}
	fmt.Fprintln(w, xfercsv.Header)
	begin := time.Now()
	for round := 1; ; round++ {
		for i, n := range sizes {
			if gap > 0 && (round > 1 || i > 0) {
				time.Sleep(gap)
			}
			start := time.Now()
			conn := shared
			if !reuse {
				c, err := net.Dial("tcp", addr)
				if err != nil {
					return err
				}
				conn = c
			}
			err := xfer.Send(conn, n)
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
