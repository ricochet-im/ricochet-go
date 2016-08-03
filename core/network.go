package core

import (
	"errors"
	"github.com/special/notricochet/core/utils"
	"github.com/special/notricochet/rpc"
	"github.com/yawning/bulb"
	bulbutils "github.com/yawning/bulb/utils"
	"log"
	"sync"
	"time"
)

type Network struct {
	// Connection settings; can only change while stopped
	controlAddress  string
	controlPassword string

	// Events
	events *utils.Publisher

	// nil when stopped, otherwise used to signal stop to active network
	stopSignal    chan struct{}
	stoppedSignal chan struct{}

	// Mutex required to access below
	controlMutex sync.Mutex

	// Do not use while holding controlMutex; instead, copy ptr and unlock
	// mutex before use.
	conn *bulb.Conn

	// Modifications must be done while holding controlMutex and signalled
	// to events. Do not directly modify the child elements, as they are
	// pointers and may be shared. Instead, construct a new TorControlStatus
	// et al for each change.
	status ricochet.NetworkStatus
}

func CreateNetwork() *Network {
	return &Network{
		events: utils.CreatePublisher(),
	}
}

// Start connection to the tor control port at 'address', with the optional
// control password 'password'. This function blocks until the first connection
// attempt is finished. The first return value says whether the connection has
// been started; if true, the connection is up even if the first attempt failed.
// The second return value is the connection attempt error, or nil on success.
func (n *Network) Start(address, password string) (bool, error) {
	n.controlMutex.Lock()
	if n.stoppedSignal != nil {
		// This is an error, because address/password might not be the same
		n.controlMutex.Unlock()
		return false, errors.New("Network is already started")
	}

	n.stopSignal = make(chan struct{})
	n.stoppedSignal = make(chan struct{})
	n.controlAddress = address
	n.controlPassword = password
	n.controlMutex.Unlock()

	connectChannel := make(chan error)
	go n.run(connectChannel)
	err := <-connectChannel
	return true, err
}

// Stop the network connection. The externally-controlled tor instance
// is not affected, but the control port connection will be closed and
// the client will be offline until Start is called again. This call will
// block until the connection is stopped.
func (n *Network) Stop() {
	// Take mutex, copy channels, nil stopSignal to avoid race if Stop()
	// is called again. Other calls will still use stoppedSignal.
	n.controlMutex.Lock()
	stop := n.stopSignal
	stopped := n.stoppedSignal
	n.stopSignal = nil
	n.controlMutex.Unlock()

	if stop != nil {
		// Signal to stop
		stop <- struct{}{}
	} else if stopped == nil {
		// Already stopped
		return
	}

	// Wait until stopped; safe for multiple receivers, because the channel
	// is closed on stop. Sender is responsible for all other cleanup and state.
	<-stopped
}

func (n *Network) EventMonitor() utils.Subscribable {
	return n.events
}

func (n *Network) GetStatus() ricochet.NetworkStatus {
	n.controlMutex.Lock()
	status := n.status
	n.controlMutex.Unlock()
	return status
}

func (n *Network) run(connectChannel chan<- error) {
	n.controlMutex.Lock()
	stopSignal := n.stopSignal
	stoppedSignal := n.stoppedSignal
	n.controlMutex.Unlock()

	for {
		// Status to CONNECTING
		n.controlMutex.Lock()
		n.status.Control = &ricochet.TorControlStatus{
			Status: ricochet.TorControlStatus_CONNECTING,
		}
		n.status.Connection = &ricochet.TorConnectionStatus{}
		status := n.status
		n.controlMutex.Unlock()
		n.events.Publish(status)

		// Attempt connection
		conn, err := createConnection(n.controlAddress, n.controlPassword)

		// Report result of the first connection attempt
		// XXX too early, because of post-connection work
		if connectChannel != nil {
			connectChannel <- err
			close(connectChannel)
			connectChannel = nil
		}

		retryChannel := make(chan error, 1)

		if err == nil {
			// Connected successfully; spawn goroutine to poll and handle
			// control events. On connection failure (or close as a result of
			// stop), signal retryChannel.

			// XXX TODO: post-connect queries

			// Status to CONNECTED
			n.controlMutex.Lock()
			n.conn = conn
			n.status.Control = &ricochet.TorControlStatus{
				Status:     ricochet.TorControlStatus_CONNECTED,
				TorVersion: "XXX", // XXX
			}
			// XXX Fake connection status
			n.status.Connection = &ricochet.TorConnectionStatus{
				Status: ricochet.TorConnectionStatus_READY,
			}
			status := n.status
			n.controlMutex.Unlock()
			n.events.Publish(status)

			go func() {
				for {
					event, err := conn.NextEvent()
					if err != nil {
						log.Printf("Control connection failed: %v", err)
						retryChannel <- err
						break
					}
					// XXX handle event
					log.Printf("Control event: %v", event)
				}
			}()
		} else {
			// Status to ERROR
			n.controlMutex.Lock()
			n.status.Control = &ricochet.TorControlStatus{
				Status:       ricochet.TorControlStatus_ERROR,
				ErrorMessage: err.Error(),
			}
			n.status.Connection = &ricochet.TorConnectionStatus{}
			status := n.status
			n.controlMutex.Unlock()
			n.events.Publish(status)

			// signal for retry in 5 seconds
			go func() {
				time.Sleep(5 * time.Second)
				retryChannel <- err
			}()
		}

		// Wait for network stop, connection failure, or retry timeout
		select {
		case <-stopSignal:
			// Clean up struct
			n.controlMutex.Lock()
			n.controlAddress = ""
			n.controlPassword = ""
			n.conn = nil
			n.stoppedSignal = nil
			n.status = ricochet.NetworkStatus{}
			n.controlMutex.Unlock()
			n.events.Publish(ricochet.NetworkStatus{})

			// Close connection
			if conn != nil {
				conn.Close()
			}

			// Signal stopped and exit
			close(stoppedSignal)
			return
		case err := <-retryChannel:
			if err == nil {
				err = errors.New("Unknown error")
			}

			// Clean up connection if necessary
			if conn != nil {
				// Status to ERROR
				n.controlMutex.Lock()
				n.conn = nil
				n.status.Control = &ricochet.TorControlStatus{
					Status:       ricochet.TorControlStatus_ERROR,
					ErrorMessage: err.Error(),
				}
				n.status.Connection = &ricochet.TorConnectionStatus{}
				status := n.status
				n.controlMutex.Unlock()
				n.events.Publish(status)

				conn.Close()
			}
			// Loop to retry connection
		}
	}
}

func createConnection(address, password string) (*bulb.Conn, error) {
	net, addr, err := bulbutils.ParseControlPortString(address)
	if err != nil {
		log.Printf("Parsing control network address '%s' failed: %v", address, err)
		return nil, err
	}

	conn, err := bulb.Dial(net, addr)
	if err != nil {
		log.Printf("Control connection failed: %v", err)
		return nil, err
	}

	err = conn.Authenticate(password)
	if err != nil {
		log.Printf("Control authentication failed: %v", err)
		conn.Close()
		return nil, err
	}

	conn.StartAsyncReader()

	// XXX
	if _, err := conn.Request("SETEVENTS STATUS_CLIENT"); err != nil {
		log.Printf("Control events failed: %v", err)
		conn.Close()
		return nil, err
	}

	log.Print("Control connected!")
	return conn, nil
}
