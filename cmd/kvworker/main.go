// Command kvworker runs a KVFlow worker: it takes Transfer commands from the
// controller over gRPC and receives KV bytes from peers over plain TCP.
//
// There is no TLS or authentication. Anyone who can reach the gRPC port
// can make this worker send bytes to any address, so expose that port only
// to the controller.
package main

import (
	"flag"
	"log"
	"net"

	"google.golang.org/grpc"

	"github.com/raghavs6/KVFlow/internal/worker"
	"github.com/raghavs6/KVFlow/internal/workerpb"
	"github.com/raghavs6/KVFlow/internal/xfer"
)

// Usage:
//
//	kvworker [-grpc :7000] [-data :9000]
func main() {
	grpcAddr := flag.String("grpc", ":7000", "address to take controller commands on")
	dataAddr := flag.String("data", ":9000", "address to receive KV bytes from peers on")
	flag.Parse()

	dataLn, err := net.Listen("tcp", *dataAddr)
	if err != nil {
		log.Fatal(err)
	}
	grpcLn, err := net.Listen("tcp", *grpcAddr)
	if err != nil {
		log.Fatal(err)
	}
	go func() { log.Fatal(xfer.ServeAll(dataLn, log.Printf)) }()
	log.Printf("kvworker: commands on %s, data on %s", grpcLn.Addr(), dataLn.Addr())
	log.Fatal(newServer().Serve(grpcLn))
}

// newServer returns a gRPC server whose Transfer calls run on a new Worker.
func newServer() *grpc.Server {
	s := grpc.NewServer()
	workerpb.RegisterWorkerServer(s, worker.New())
	return s
}
