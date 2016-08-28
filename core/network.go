package core

import (
	"errors"
	"github.com/special/notricochet/core/utils"
	"github.com/special/notricochet/rpc"
	"github.com/yawning/bulb"
	bulbutils "github.com/yawning/bulb/utils"
	"log"
	"strings"
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

		retryChannel := make(chan error, 1)

		if err == nil {
			// Connected successfully; spawn goroutine to poll and handle
			// control events. On connection failure (or close as a result of
			// stop), signal retryChannel.

			// Query ProtocolInfo for tor version
			pinfo, err := conn.ProtocolInfo()
			if err != nil {
				log.Printf("Control protocolinfo failed: %v", err)
				retryChannel <- err
			} else {
				// Status to CONNECTED
				n.controlMutex.Lock()
				n.conn = conn
				n.status.Control = &ricochet.TorControlStatus{
					Status:     ricochet.TorControlStatus_CONNECTED,
					TorVersion: pinfo.TorVersion,
				}
				n.status.Connection = &ricochet.TorConnectionStatus{}
				status := n.status
				n.controlMutex.Unlock()
				n.events.Publish(status)

				// Query initial tor state and subscribe to events
				if err := n.updateTorState(); err != nil {
					log.Printf("Control state query failed: %v", err)
					// Signal error to terminate connection
					retryChannel <- err
				} else {
					// Report result of the first connection attempt
					if connectChannel != nil {
						connectChannel <- err
						close(connectChannel)
						connectChannel = nil
					}

					// Goroutine polls for control events; retryChannel is
					// signalled on connection failure. Block on retryChannel
					// below.
					go n.handleControlEvents(conn, retryChannel)
				}
			}
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

	log.Print("Control connected!")
	return conn, nil
}

/* XXX The CIRCUIT_ESTABLISHED based connectivity logic is buggy and not
 * reliable. We may not see CIRCUIT_ESTABLISHED if tor goes dormant due to
 * no activity, and CIRCUIT_NOT_ESTABLISHED is _only_ sent for clock jumps,
 * not any other case. For now, this is still worth using, because it at
 * least gives a decent idea of when startup has finished and detects
 * suspends from the clock jump.
 *
 * Tor also has a NETWORK_LIVENESS, but this is even less useful. In testing,
 * it's entirely unable to determine when tor loses connectivity.
 *
 * The most reliable indicator of connectivity is probably to track active
 * circs or orconns and assume connectivity if there is at least one built or
 * connected. This is a little more complex, but would give us better behavior
 * for figuring out when reconnection is necessary and whether we're connectable.
 * If we start tracking circuits, we could also use those to gain more insight
 * into the connectivity state of our services, the number of rendezvous, and
 * reasons for failed outbound connections.
 */

func (n *Network) updateTorState() error {
	if _, err := n.conn.Request("SETEVENTS STATUS_CLIENT"); err != nil {
		return err
	}

	response, err := n.conn.Request("GETINFO status/circuit-established status/bootstrap-phase net/listeners/socks")
	if err != nil {
		return err
	}

	results := make(map[string]string)
	for _, rawLine := range response.Data {
		line := strings.SplitN(rawLine, "=", 2)
		if len(line) != 2 {
			return errors.New("Invalid GETINFO response format")
		}
		results[line[0]] = strings.TrimSpace(line[1])
		log.Printf("'%v' = '%v'", line[0], results[line[0]])
	}

	var connStatus ricochet.TorConnectionStatus_Status
	if results["status/circuit-established"] == "0" {
		if strings.Contains(results["status/bootstrap-phase"], "TAG=done") {
			connStatus = ricochet.TorConnectionStatus_OFFLINE
		} else {
			connStatus = ricochet.TorConnectionStatus_BOOTSTRAPPING
		}
	} else if results["status/circuit-established"] == "1" {
		connStatus = ricochet.TorConnectionStatus_READY
	} else {
		return errors.New("Invalid GETINFO response format")
	}

	socksAddresses := utils.UnquoteStringSplit(results["net/listeners/socks"], ' ')

	n.controlMutex.Lock()
	n.status.Connection = &ricochet.TorConnectionStatus{
		Status:            connStatus,
		BootstrapProgress: results["status/bootstrap-phase"],
		SocksAddress:      socksAddresses,
	}
	status := n.status
	n.controlMutex.Unlock()
	n.events.Publish(status)

	return nil
}

func (n *Network) handleControlEvents(conn *bulb.Conn, errorChannel chan<- error) {
	for {
		event, err := conn.NextEvent()
		if err != nil {
			log.Printf("Control connection failed: %v", err)
			errorChannel <- err
			return
		}

		if strings.HasPrefix(event.Reply, "STATUS_CLIENT ") ||
			strings.HasPrefix(event.Reply, "STATUS_GENERAL ") {
			// StatusType StatusSeverity StatusAction StatusArguments
			eventInfo := strings.SplitN(event.Reply, " ", 4)
			if len(eventInfo) < 3 {
				log.Printf("Ignoring malformed control status event")
				continue
			}

			n.controlMutex.Lock()
			changed := true
			// Cannot directly modify n.status.Connection, because it may be shared; take a copy
			connStatus := *n.status.Connection

			if eventInfo[2] == "CIRCUIT_ESTABLISHED" {
				connStatus.Status = ricochet.TorConnectionStatus_READY
			} else if eventInfo[2] == "CIRCUIT_NOT_ESTABLISHED" {
				if strings.Contains(connStatus.BootstrapProgress, "TAG=done") {
					connStatus.Status = ricochet.TorConnectionStatus_OFFLINE
				} else {
					connStatus.Status = ricochet.TorConnectionStatus_BOOTSTRAPPING
				}
			} else if eventInfo[2] == "BOOTSTRAP" {
				connStatus.BootstrapProgress = strings.Join(eventInfo[1:], " ")
			} else {
				changed = false
			}

			if changed {
				n.status.Connection = &connStatus
				status := n.status
				n.controlMutex.Unlock()
				n.events.Publish(status)
			} else {
				n.controlMutex.Unlock()
			}
		}
	}
}
