package main

import (
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

func main() {
	conn, err := grpc.Dial(defaultAddress, grpc.WithInsecure())
	if err != nil {
		fmt.Printf("connection failed: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	input, err := readline.NewEx(&readline.Config{})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer input.Close()
	log.SetOutput(input.Stdout())

	client := &Client{
		Backend: rpc.NewRicochetCoreClient(conn),
	}
	ui := &UI{
		Input:  input,
		Client: client,
	}
	client.Ui = ui

	if err := client.Initialize(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	go client.Run()
	ui.CommandLoop()
}
