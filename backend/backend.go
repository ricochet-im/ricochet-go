package main

import (
	rpc "github.com/special/ricochet-go/rpc"
	"google.golang.org/grpc"
	"log"
	"net"
)

const (
	defaultListener = "127.0.0.1:58281"
)

func main() {
	listener, err := net.Listen("tcp", defaultListener)
	if err != nil {
		log.Fatalf("listen failed: %v", err)
	}
	rpcServer := grpc.NewServer()
	rpc.RegisterRicochetCoreServer(rpcServer, &RicochetCore{})
	rpcServer.Serve(listener)
}
