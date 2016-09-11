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

	core := new(ricochet.Ricochet)
	if err := core.Init(config); err != nil {
		log.Fatalf("init error: %v", err)
	}

	server := &RpcServer{
		core: core,
	}
	grpcServer := grpc.NewServer()
	rpc.RegisterRicochetCoreServer(grpcServer, server)
	grpcServer.Serve(listener)
}
