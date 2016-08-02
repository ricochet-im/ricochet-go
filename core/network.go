package core

import (
	"errors"
	"github.com/special/notricochet/core/utils"
	"github.com/yawning/bulb"
	bulbutils "github.com/yawning/bulb/utils"
	"log"
	"sync"
	"time"
)

type Network struct {
	conn *bulb.Conn

	controlAddress  string
	controlPassword string

	handleLock  sync.Mutex
	stopChannel chan struct{}

	events *utils.Publisher
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
	n.handleLock.Lock()
	defer n.handleLock.Unlock()

	if n.stopChannel != nil {
		// This is an error, because address/password might not be the same
		return false, errors.New("Network is already started")
	}

	n.stopChannel = make(chan struct{})
	n.controlAddress = address
	n.controlPassword = password

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
	n.handleLock.Lock()
	defer n.handleLock.Unlock()

	if n.stopChannel == nil {
		return
	}

	// XXX but if we do block that long, we can hold the mutex a _long_ time.
	// XXX so the mutex won't be suitable for a "is network started" check
	n.stopChannel <- struct{}{}
	n.stopChannel = nil
	n.conn = nil
	n.controlAddress = ""
	n.controlPassword = ""
}

func (n *Network) EventMonitor() utils.Subscribable {
	return n.events
}

func (n *Network) run(connectChannel chan<- error) {
	var conn *bulb.Conn
	var err error

	for {
		conn, err = createConnection(n.controlAddress, n.controlPassword)

		// Report result of the first connection attempt
		if connectChannel != nil {
			connectChannel <- err
			close(connectChannel)
			connectChannel = nil
		}

		retryChannel := make(chan struct{}, 1)

		if err == nil {
			// Success! Set connection, handle events, and retry connection
			// if it fails
			n.conn = conn
			n.events.Publish(true)
			go func() {
				for {
					event, err := conn.NextEvent()
					if err != nil {
						log.Printf("Control connection failed: %v", err)
						n.events.Publish(false)
						retryChannel <- struct{}{}
						break
					}
					log.Printf("Control event: %v", event)
				}
			}()
		} else {
			// Failed; retry in one second
			go func() {
				time.Sleep(1 * time.Second)
				retryChannel <- struct{}{}
			}()
		}

		select {
		case <-n.stopChannel:
			break
		case <-retryChannel:
			if conn != nil {
				conn.Close()
			}
		}
	}

	// Stopped
	if conn != nil {
		conn.Close()
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
