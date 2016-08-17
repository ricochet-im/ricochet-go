package main

import (
	"fmt"
	rpc "github.com/special/notricochet/rpc"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"gopkg.in/readline.v1"
	"io"
	"log"
	"strings"
)

const (
	defaultAddress = "127.0.0.1:58281"
)

type Client struct {
	Backend rpc.RicochetCoreClient
	Input   *readline.Instance
}

func (c *Client) Initialize() error {
	stream, err := c.Backend.MonitorNetwork(context.Background(), &rpc.MonitorNetworkRequest{})
	if err != nil {
		return err
	}

	go func() {
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				log.Printf("stream eof")
				break
			}
			if err != nil {
				log.Printf("stream error: %v", err)
				break
			}
			log.Printf("stream says: %v", resp)
		}
	}()

	return nil
}

func main() {
	conn, err := grpc.Dial(defaultAddress, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("connection failed: %v", err)
	}
	defer conn.Close()

	input, err := readline.NewEx(&readline.Config{})
	if err != nil {
		log.Fatal(err)
	}
	defer input.Close()
	log.SetOutput(input.Stdout())

	c := &Client{
		Backend: rpc.NewRicochetCoreClient(conn),
		Input:   input,
	}

	if err := c.Initialize(); err != nil {
		log.Fatal(err)
	}

	input.SetPrompt("> ")
	for {
		line := input.Line()
		if line.CanContinue() {
			continue
		} else if line.CanBreak() {
			break
		}

		words := strings.SplitN(line.Line, " ", 1)
		switch words[0] {
		case "clear":
			readline.ClearScreen(readline.Stdout)
		case "status":
			r, err := c.Backend.GetServerStatus(context.Background(), &rpc.ServerStatusRequest{
				RpcVersion: 1,
			})
			if err != nil {
				log.Fatalf("could not get status: %v", err)
			}
			log.Printf("get status result: %v", r)
		case "connect":
			status, err := c.Backend.StartNetwork(context.Background(), &rpc.StartNetworkRequest{})
			if err != nil {
				log.Printf("start network error: %v", err)
			} else {
				log.Printf("network started: %v", status)
			}
		case "disconnect":
			status, err := c.Backend.StopNetwork(context.Background(), &rpc.StopNetworkRequest{})
			if err != nil {
				log.Printf("stop network error: %v", err)
			} else {
				log.Printf("network stopped: %v", status)
			}
		case "help":
			fallthrough
		default:
			fmt.Println("Commands: clear, status, connect, disconnect, help")
		}
	}
}
