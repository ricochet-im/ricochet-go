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
	backendConnect string
	backendServer  string
	unsafeBackend  bool
	backendMode    bool
	connectAuto    bool
	configPath     string = "identity.ricochet"
	torAddress     string
	torPassword    string
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s [<args>] [<identity>]\n\tStandalone client using <identity> (default \"./identity.ricochet\")\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -listen <address> [<args>] [<identity>]\n\tListen on <address> for Ricochet client frontend connections\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -attach <address> [<args>]\n\tAttach to a client backend running on <address>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nArgs:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n")
	}
	flag.StringVar(&configPath, "identity", configPath, "Load identity from `<file>`")
	flag.StringVar(&backendConnect, "attach", "", "Attach to the client backend running on `<address>`")
	flag.StringVar(&backendServer, "listen", "", "Listen on `<address>` for client frontend connections")
	flag.BoolVar(&unsafeBackend, "allow-unsafe-backend", false, "Allow a remote backend address. This is NOT RECOMMENDED and may harm your security or privacy. Do not use without a secure, trusted link")
	flag.BoolVar(&backendMode, "only-backend", false, "Run backend without any commandline UI")
	flag.BoolVar(&connectAuto, "connect", true, "Start connecting to the network automatically")
	flag.StringVar(&torAddress, "tor-control", "", "Use the tor control port at `<address>`, which may be 'host:port' or 'unix:/path'")
	flag.StringVar(&torPassword, "tor-control-password", "", "Use `<password>` to authenticate to the tor control port")
	flag.Parse()
	if len(flag.Args()) > 1 {
		flag.Usage()
		os.Exit(1)
	} else if len(flag.Args()) == 1 {
		configPath = flag.Arg(0)
	}

	// Check for flag combinations that make no sense
	if backendConnect != "" {
		if backendMode {
			fmt.Printf("Cannot use -only-backend with -attach, because attach implies not running a backend\n")
			os.Exit(1)
		} else if backendServer != "" {
			fmt.Printf("Cannot use -listen with -attach, because attach implies not running a backend\n")
			os.Exit(1)
		} else if configPath != "identity.ricochet" {
			fmt.Printf("Cannot use -identity with -attach, because identity is stored at the backend\n")
			os.Exit(1)
		} else if torAddress != "" || torPassword != "" {
			fmt.Printf("Cannot use -tor-control with -attach, because tor connections happen on the backend\n")
			os.Exit(1)
		}
	}

	// Redirect log before starting backend, unless in backend mode
	if !backendMode {
		log.SetOutput(&LogBuffer)
	}

	// Unless backendConnect is set, start the in-process backend
	if backendConnect == "" {
		err := startBackend()
		if err != nil {
			fmt.Printf("backend failed: %v\n", err)
			os.Exit(1)
		}

		if backendMode {
			// Block forever; backend uses os.Exit on failure
			c := make(chan struct{})
			<-c
			os.Exit(0)
		}
	}

	// Connect to RPC backend
	conn, err := connectClientBackend()
	if err != nil {
		fmt.Printf("backend connection failed: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

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
	address := backendConnect
	if address == "" {
		// If not explicitly specified, use the listen address. If that is also empty, default to in-process
		address = backendServer
		if address == "" {
			// In-process backend, using 'InnerNet' as a fake socket
			return grpc.Dial("ricochet.rpc", grpc.WithInsecure(), grpc.WithDialer(DialInnerNet))
		}
	}

	// External backend
	if strings.HasPrefix(address, "unix:") {
		return grpc.Dial(address[5:], grpc.WithInsecure(),
			grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
				return net.DialTimeout("unix", addr, timeout)
			}))
	} else {
		if err := checkBackendAddressSafety(address); err != nil {
			return nil, err
		}
		return grpc.Dial(address, grpc.WithInsecure())
	}
}

func checkBackendAddressSafety(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if !unsafeBackend && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("Host '%s' is not a loopback address.\nRead the warnings and use -allow-unsafe-backend for non-local addresses", host)
	}
	return nil
}

func startBackend() error {
	config, err := ricochet.LoadConfig(configPath)
	if err != nil {
		return err
	}

	core := new(ricochet.Ricochet)
	if err := core.Init(config); err != nil {
		return err
	}

	if torAddress != "" {
		core.Network.SetControlAddress(torAddress)
	}
	if torPassword != "" {
		core.Network.SetControlPassword(torPassword)
	}

	var listener net.Listener
	if backendServer == "" {
		// In-process backend, using 'InnerNet' as a fake socket
		listener, err = ListenInnerNet("ricochet.rpc")
	} else {
		if strings.HasPrefix(backendServer, "unix:") {
			// XXX Need the right behavior for cleaning up old sockets, permissions, etc
			listener, err = net.Listen("unix", backendServer[5:])
		} else {
			if err := checkBackendAddressSafety(backendServer); err != nil {
				return err
			}
			listener, err = net.Listen("tcp", backendServer)
		}
	}
	if err != nil {
		return err
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

	if connectAuto {
		go func() {
			core.Network.Start()
		}()
	}

	return nil
}
