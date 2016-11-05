package main

import (
	"bytes"
	"flag"
	"fmt"
	"github.com/chzyer/readline"
	ricochet "github.com/ricochet-im/ricochet-go/core"
	rpc "github.com/ricochet-im/ricochet-go/rpc"
	"google.golang.org/grpc"
	"log"
	"net"
	"os"
	"strings"
	"time"
)

var (
	LogBuffer bytes.Buffer

	// Flags
	backendAddress string
	unsafeBackend  bool
)

func main() {
	flag.StringVar(&backendAddress, "backend", "", "Connect to the client backend running on `address`")
	flag.BoolVar(&unsafeBackend, "allow-unsafe-backend", false, "Allow a remote backend address. This is NOT RECOMMENDED and may harm your security or privacy. Do not use without a secure, trusted link")
	flag.Parse()

	// Set up readline
	input, err := readline.NewEx(&readline.Config{
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer input.Close()
	log.SetOutput(&LogBuffer)

	// Connect to RPC backend, start in-process backend if necessary
	conn, err := connectClientBackend()
	if err != nil {
		fmt.Printf("backend failed: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	// Configure client and UI
	client := &Client{
		Backend: rpc.NewRicochetCoreClient(conn),
	}
	Ui = UI{
		Input:  input,
		Stdout: input.Stdout(),
		Client: client,
	}

	// Initialize data from backend and start UI command loop
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

func connectClientBackend() (*grpc.ClientConn, error) {
	if backendAddress == "" {
		// In-process backend, using 'InnerNet' as a fake socket
		address, err := startLocalBackend()
		if err != nil {
			return nil, err
		}
		return grpc.Dial(address, grpc.WithInsecure(), grpc.WithDialer(DialInnerNet))
	} else {
		// External backend
		if strings.HasPrefix(backendAddress, "unix:") {
			return grpc.Dial(backendAddress[5:], grpc.WithInsecure(),
				grpc.WithDialer(func(address string, timeout time.Duration) (net.Conn, error) {
					return net.DialTimeout("unix", address, timeout)
				}))
		} else {
			host, _, err := net.SplitHostPort(backendAddress)
			if err != nil {
				return nil, err
			}
			ip := net.ParseIP(host)
			if !unsafeBackend && (ip == nil || !ip.IsLoopback()) {
				return nil, fmt.Errorf("Host '%s' is not a loopback address.\nRead the warnings and use -allow-unsafe-backend for non-local addresses", host)
			}

			return grpc.Dial(backendAddress, grpc.WithInsecure())
		}
	}
}

func startLocalBackend() (string, error) {
	config, err := ricochet.LoadConfig(".")
	if err != nil {
		return "", err
	}

	core := new(ricochet.Ricochet)
	if err := core.Init(config); err != nil {
		return "", err
	}

	listener, err := ListenInnerNet("ricochet.rpc")
	if err != nil {
		return "", err
	}

	server := &ricochet.RpcServer{
		Core: core,
	}

	go func() {
		grpcServer := grpc.NewServer()
		rpc.RegisterRicochetCoreServer(grpcServer, server)
		err := grpcServer.Serve(listener)
		if err != nil {
			log.Printf("backend exited: %v", err)
			os.Exit(1)
		}
	}()

	return "ricochet.rpc", nil
}
