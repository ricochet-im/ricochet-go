package main

import (
	ricochet "github.com/special/ricochet-go/core"
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

	config, err := ricochet.LoadConfig(".")
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	ricochet := &RicochetCore{
		Network: ricochet.CreateNetwork(),
		Config:  config,
	}

	rpcServer := grpc.NewServer()
	rpc.RegisterRicochetCoreServer(rpcServer, ricochet)
	rpcServer.Serve(listener)
}
