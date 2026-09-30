// Command kvworker runs a KVFlow worker: it takes Transfer commands from the
// controller over gRPC and receives KV bytes from peers over plain TCP.
//
// There is no TLS or authentication. The worker only sends to the peers
// named in -peers, so it can't be made to flood an arbitrary address, but
// anyone who can reach the gRPC port can still make it send to those
// peers. Expose that port only to the controller.
package main

import (
	"flag"
	"log"
	"net"
	"strings"

	"google.golang.org/grpc"

	"github.com/raghavs6/KVFlow/internal/worker"
	"github.com/raghavs6/KVFlow/internal/workerpb"
	"github.com/raghavs6/KVFlow/internal/xfer"
)

// Usage:
//
//	kvworker -peers host:port[,host:port...] [-grpc 127.0.0.1:50051] [-data 127.0.0.1:9000]
//
// Both addresses default to this machine only; listening on a network, such
// as with -grpc 0.0.0.0:50051, has to be asked for. 50051 rather than 7000
// because macOS's AirPlay Receiver already holds 7000.
func main() {
	grpcAddr := flag.String("grpc", "127.0.0.1:50051", "address to take controller commands on")
	dataAddr := flag.String("data", "127.0.0.1:9000", "address to receive KV bytes from peers on")
	peersFlag := flag.String("peers", "", "comma-separated peer data addresses this worker may send to, matched exactly")
	flag.Parse()

	var peers []string
	if *peersFlag != "" {
		peers = strings.Split(*peersFlag, ",")
	} else {
		log.Print("kvworker: no -peers given, so every Transfer will be rejected")
	}

	dataLn, err := net.Listen("tcp", *dataAddr)
	if err != nil {
		log.Fatal(err)
	}
	grpcLn, err := net.Listen("tcp", *grpcAddr)
	if err != nil {
		log.Fatal(err)
	}
	go func() { log.Fatal(xfer.ServeAll(dataLn, log.Printf)) }()
	log.Printf("kvworker: commands on %s, data on %s, sending to %q", grpcLn.Addr(), dataLn.Addr(), peers)
	log.Fatal(newServer(peers).Serve(grpcLn))
}

// newServer returns a gRPC server whose Transfer calls run on a new Worker
// that may send only to peers.
func newServer(peers []string) *grpc.Server {
	s := grpc.NewServer()
	workerpb.RegisterWorkerServer(s, worker.New(peers))
	return s
}
