// Package workerpb holds the Go code generated from worker.proto.
//
// Regenerate with `go generate ./internal/workerpb` after installing:
//
//	brew install protobuf  # protoc 36.2
//	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
//	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
package workerpb

//go:generate protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative worker.proto
