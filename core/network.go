package core

import (
	"crypto"
	"errors"
	"github.com/special/notricochet/core/utils"
	"github.com/special/notricochet/rpc"
	"github.com/yawning/bulb"
	bulbutils "github.com/yawning/bulb/utils"
	"log"
	"net"
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

	onions []*OnionService
}

type OnionService struct {
	OnionID    string
	Ports      []bulb.OnionPortSpec
	PrivateKey crypto.PrivateKey
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

// Return the control connection, blocking until connected if necessary
// May return nil on failure, and the returned connection can be closed
// or otherwise fail at any time.
func (n *Network) getConnection() *bulb.Conn {
	// Optimistically try to get a connection before subscribing to events
	n.controlMutex.Lock()
	conn := n.conn
	n.controlMutex.Unlock()
	if conn != nil {
		return conn
	}

	// Subscribe to connectivity change events
	monitor := n.EventMonitor().Subscribe(20)
	defer n.EventMonitor().Unsubscribe(monitor)

	for {
		// Check for connectivity; do this before blocking to avoid a
		// race with the subscription.
		n.controlMutex.Lock()
		conn := n.conn
		n.controlMutex.Unlock()
		if conn != nil {
			return conn
		}

		_, ok := <-monitor
		if !ok {
			return nil
		}
	}
}

// Add an onion service with the provided port mappings and private key.
// If key is nil, a new RSA key is generated and returned in OnionService.
// This function will block until a control connection is available and
// the service is added or the command has failed. If the control connection
// is lost and reconnected, the service will be re-added automatically.
// BUG(special): Errors that occur after reconnecting cannot be detected.
func (n *Network) AddOnionPorts(ports []bulb.OnionPortSpec, key crypto.PrivateKey) (*OnionService, error) {
	if key == nil {
		// Ask for a new key, force RSA1024
		key = &bulb.OnionPrivateKey{
			KeyType: "NEW",
			Key:     "RSA1024",
		}
	}

	conn := n.getConnection()
	info, err := conn.AddOnion(ports, key, false)
	if err != nil {
		return nil, err
	}

	service := &OnionService{
		OnionID:    info.OnionID,
		Ports:      ports,
		PrivateKey: info.PrivateKey,
	}

	if service.PrivateKey == nil {
		service.PrivateKey = key
	}

	n.controlMutex.Lock()
	n.onions = append(n.onions, service)
	n.controlMutex.Unlock()
	return service, nil
}

// Add an onion service listening on the virtual onion port onionPort,
// with the provided private key, and return a net.Listener for it. This
// function behaves identically to AddOnionPorts, other than creating a
// listener automatically.
func (n *Network) NewOnionListener(onionPort uint16, key crypto.PrivateKey) (*OnionService, net.Listener, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}

	onionPorts := []bulb.OnionPortSpec{
		bulb.OnionPortSpec{
			VirtPort: onionPort,
			Target:   listener.Addr().String(),
		},
	}

	service, err := n.AddOnionPorts(onionPorts, key)
	if err != nil {
		listener.Close()
		return nil, nil, err
	}

	return service, listener, nil
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
		errorChannel := make(chan error, 1)
		err := n.connectControl()
		if err != nil {
			errorChannel <- err
		} else {
			// The goroutine polls for control events, and signals
			// errorChannel on connection failure.
			go n.handleControlEvents(n.conn, errorChannel)
		}

		// Report result of the first connection attempt
		if connectChannel != nil {
			connectChannel <- err
			close(connectChannel)
			connectChannel = nil
		}

		// Wait for network stop or connection errors
		select {
		case <-stopSignal:
			// Close connection, clean up struct, signal status change
			n.controlMutex.Lock()
			if n.conn != nil {
				n.conn.Close()
				n.conn = nil
			}
			n.controlAddress = ""
			n.controlPassword = ""
			n.stoppedSignal = nil
			n.status = ricochet.NetworkStatus{}
			n.controlMutex.Unlock()
			n.events.Publish(ricochet.NetworkStatus{})

			// Signal stopped and exit
			close(stoppedSignal)
			return

		case err := <-errorChannel:
			if err == nil {
				err = errors.New("Unknown error")
			}

			// Change status to ERROR
			n.controlMutex.Lock()
			if n.conn != nil {
				n.conn.Close()
				n.conn = nil
			}
			n.status.Control = &ricochet.TorControlStatus{
				Status:       ricochet.TorControlStatus_ERROR,
				ErrorMessage: err.Error(),
			}
			n.status.Connection = &ricochet.TorConnectionStatus{}
			status := n.status
			n.controlMutex.Unlock()
			n.events.Publish(status)

			// Loop to retry connection
			// BUG(x): This timeout is static and uninterruptable
			time.Sleep(5 * time.Second)
		}
	}
}

func (n *Network) connectControl() error {
	// Attempt connection
	conn, err := createConnection(n.controlAddress, n.controlPassword)
	if err != nil {
		return err
	}

	// Query ProtocolInfo for tor version
	pinfo, err := conn.ProtocolInfo()
	if err != nil {
		conn.Close()
		return err
	}

	// Subscribe to events
	_, err = conn.Request("SETEVENTS STATUS_CLIENT")
	if err != nil {
		conn.Close()
		return err
	}

	// Query initial tor state
	connStatus, err := queryTorState(conn)
	if err != nil {
		conn.Close()
		return err
	}

	// Copy list of onions to republish. This is done before the status
	// change to avoid racing with blocked calls to AddOnionPorts, which
	// will add to this list once the connection is available, but the
	// publication is done afterwards.
	n.controlMutex.Lock()
	onions := make([]*OnionService, len(n.onions))
	copy(onions, n.onions)
	n.controlMutex.Unlock()

	// Update network status and set connection
	n.controlMutex.Lock()
	n.conn = conn
	n.status.Control = &ricochet.TorControlStatus{
		Status:     ricochet.TorControlStatus_CONNECTED,
		TorVersion: pinfo.TorVersion,
	}
	n.status.Connection = &connStatus
	status := n.status
	n.controlMutex.Unlock()
	n.events.Publish(status)

	// Re-publish onion services. Errors are not fatal to conn.
	publishOnions(conn, onions)

	return nil
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

func queryTorState(conn *bulb.Conn) (ricochet.TorConnectionStatus, error) {
	status := ricochet.TorConnectionStatus{}

	response, err := conn.Request("GETINFO status/circuit-established status/bootstrap-phase net/listeners/socks")
	if err != nil {
		return status, err
	}

	results := make(map[string]string)
	for _, rawLine := range response.Data {
		line := strings.SplitN(rawLine, "=", 2)
		if len(line) != 2 {
			return status, errors.New("Invalid GETINFO response format")
		}
		results[line[0]] = strings.TrimSpace(line[1])
	}

	if results["status/circuit-established"] == "0" {
		if strings.Contains(results["status/bootstrap-phase"], "TAG=done") {
			status.Status = ricochet.TorConnectionStatus_OFFLINE
		} else {
			status.Status = ricochet.TorConnectionStatus_BOOTSTRAPPING
		}
	} else if results["status/circuit-established"] == "1" {
		status.Status = ricochet.TorConnectionStatus_READY
	} else {
		return status, errors.New("Invalid GETINFO response format")
	}

	status.BootstrapProgress = results["status/bootstrap-phase"]
	status.SocksAddress = utils.UnquoteStringSplit(results["net/listeners/socks"], ' ')
	return status, nil
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

func publishOnions(conn *bulb.Conn, onions []*OnionService) {
	for _, service := range onions {
		_, err := conn.AddOnion(service.Ports, service.PrivateKey, false)
		if err != nil {
			log.Printf("Control error for onion republication: %v", err)
		}
		log.Printf("Re-published onion service %s", service.OnionID)
	}
}
