package main

import (
	"bytes"
	"fmt"
	"github.com/chzyer/readline"
	rpc "github.com/ricochet-im/ricochet-go/rpc"
	"google.golang.org/grpc"
	"log"
	"os"
)

const (
	defaultAddress = "127.0.0.1:58281"
)

var LogBuffer bytes.Buffer

func main() {
	input, err := readline.NewEx(&readline.Config{})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer input.Close()
	log.SetOutput(&LogBuffer)

	conn, err := grpc.Dial(defaultAddress, grpc.WithInsecure())
	if err != nil {
		fmt.Printf("connection failed: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	client := &Client{
		Backend: rpc.NewRicochetCoreClient(conn),
	}
	Ui = UI{
		Input:  input,
		Stdout: input.Stdout(),
		Client: client,
	}

	fmt.Print("Connecting to backend...\n")

	go func() {
		if err := client.Initialize(); err != nil {
			fmt.Printf("Error: %s\n", err)
			os.Exit(1)
		}
		client.Block()
		Ui.PrintStatus()
		client.Unblock()
	}()

	Ui.CommandLoop()
}
