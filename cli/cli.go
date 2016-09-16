package main

import (
	"fmt"
	rpc "github.com/special/notricochet/rpc"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"gopkg.in/readline.v1"
	"log"
	"strings"
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
			log.Printf("server: %v", c.ServerStatus)
			log.Printf("identity: %v", c.Identity)

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

		case "contacts":
			for _, contact := range c.Contacts {
				log.Printf("  %s (%s)", contact.Nickname, contact.Status.String())
			}

		case "help":
			fallthrough

		default:
			fmt.Println("Commands: clear, status, connect, disconnect, contacts, help")
		}
	}
}
