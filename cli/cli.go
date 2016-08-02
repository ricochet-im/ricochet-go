package main

import (
	rpc "github.com/special/notricochet/rpc"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"io"
	"log"
)

const (
	defaultAddress = "127.0.0.1:58281"
)

func main() {
	conn, err := grpc.Dial(defaultAddress, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("connection failed: %v", err)
	}
	defer conn.Close()

	c := rpc.NewRicochetCoreClient(conn)

	r, err := c.GetServerStatus(context.Background(), &rpc.ServerStatusRequest{
		RpcVersion: 1,
	})
	if err != nil {
		log.Fatalf("could not get status: %v", err)
	}
	log.Printf("get status result: %v", r)

	stream, err := c.MonitorNetwork(context.Background(), &rpc.MonitorNetworkRequest{})
	if err != nil {
		log.Fatalf("could not start stream: %v", err)
	}

	wait := make(chan struct{})
	go func() {
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				log.Printf("stream eof")
				break
			}
			if err != nil {
				log.Fatalf("stream error: %v", err)
			}
			log.Printf("stream says: %v", resp)
		}
		close(wait)
	}()

	status, err := c.StartNetwork(context.Background(), &rpc.StartNetworkRequest{})
	if err != nil {
		log.Printf("start network error: %v", err)
	} else {
		log.Printf("network started: %v", status)
	}

	<-wait
}
