package main

import (
	"bytes"
	"fmt"
	rpc "github.com/special/notricochet/rpc"
	"google.golang.org/grpc"
	"gopkg.in/readline.v1"
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
	ui := &UI{
		Input:  input,
		Client: client,
	}
	client.Ui = ui

	fmt.Print("Connecting to backend...\n")

	go func() {
		if err := client.Initialize(); err != nil {
			fmt.Printf("Error: %s\n", err)
			os.Exit(1)
		}
		client.Block()
		ui.PrintStatus()
		client.Unblock()
	}()

	ui.CommandLoop()
}
