package main

import (
	ricochet "github.com/special/notricochet/core"
	rpc "github.com/special/notricochet/rpc"
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

	core := &RicochetCore{
		config: config,
	}
	core.network = ricochet.CreateNetwork()
	core.identity, err = ricochet.CreateIdentity(core)
	if err != nil {
		log.Fatalf("identity error: %v", err)
	}

	rpcServer := grpc.NewServer()
	rpc.RegisterRicochetCoreServer(rpcServer, core)
	rpcServer.Serve(listener)
}
