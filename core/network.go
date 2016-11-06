package core

import (
	"crypto"
	"errors"
	"github.com/ricochet-im/ricochet-go/core/utils"
	"github.com/ricochet-im/ricochet-go/rpc"
	"github.com/yawning/bulb"
	"golang.org/x/net/context"
	"golang.org/x/net/proxy"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

// XXX Network disconnect should kill open connections ... somehow

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

	socksAddress socksAddress
	onions       []*OnionService
}

type OnionService struct {
	Network    *Network
	OnionID    string
	Ports      []bulb.OnionPortSpec
	PrivateKey crypto.PrivateKey
}

type OnionServiceListener struct {
	Service          *OnionService
	InternalListener net.Listener
}

func CreateNetwork() *Network {
	return &Network{
		events: utils.CreatePublisher(),
	}
}

func (n *Network) SetControlAddress(address string) error {
	n.controlMutex.Lock()
	defer n.controlMutex.Unlock()
	if n.stoppedSignal != nil {
		return errors.New("Network is already started")
	}

	n.controlAddress = address
	return nil
}

func (n *Network) SetControlPassword(password string) error {
	n.controlMutex.Lock()
	defer n.controlMutex.Unlock()
	if n.stoppedSignal != nil {
		return errors.New("Network is already started")
	}

	n.controlPassword = password
	return nil
}

// Start connection to the tor control port. This function blocks until the first
// connection attempt is finished. The first return value says whether the
// connection has been started; if true, the connection is up even if the first
// attempt failed. The second return value is the connection attempt error, or
// nil on success.
func (n *Network) Start() (bool, error) {
	n.controlMutex.Lock()
	if n.stoppedSignal != nil {
		n.controlMutex.Unlock()
		return false, errors.New("Network is already started")
	}
	if n.controlAddress == "" {
		n.controlMutex.Unlock()
		return false, errors.New("Control address not configured")
	}
	n.stopSignal = make(chan struct{})
	n.stoppedSignal = make(chan struct{})
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

type socksAddress struct {
	Network string
	Address string
	IP      net.IP
}

func (sa socksAddress) IsValid() bool {
	return sa.Network != "" && sa.Address != ""
}

func (sa socksAddress) PreferredTo(other socksAddress, preferredIP net.IP) bool {
	// Prefer, in order:
	//   - any over null
	//   - unix sockets over others
	//   - same ip as control address
	//   - loopback over other ips
	//   - first seen

	if sa.Network == "" || other.Network == "" {
		return other.Network == ""
	}

	if sa.Network == "unix" || other.Network == "unix" {
		return other.Network != "unix"
	}

	if !preferredIP.IsUnspecified() &&
		(net.IP.Equal(sa.IP, preferredIP) || net.IP.Equal(other.IP, preferredIP)) {
		return !net.IP.Equal(other.IP, preferredIP)
	}

	if sa.IP.IsLoopback() || other.IP.IsLoopback() {
		return !other.IP.IsLoopback()
	}

	return false
}

// Choose the best SOCKS address out of the list in 'addresses'
// If controlAddress is non-empty, prefer a SOCKS port on the same host
func chooseSocksAddress(addresses []string, controlAddress string) (socksAddress, error) {
	var selected socksAddress
	var preferredIP net.IP
	torOnLocalhost := true

	if len(addresses) == 0 {
		return selected, errors.New("No SOCKS port configured")
	}

	if !strings.HasPrefix(controlAddress, "unix:") {
		addr, _, _ := net.SplitHostPort(controlAddress)
		preferredIP = net.ParseIP(addr)
		torOnLocalhost = preferredIP.IsLoopback()
	}

	// List of SOCKS ports, relative to the tor daemon
	// Can be in the form "127.0.0.1:9050", "unix:...", or "[::1]:9050"
	for _, addr := range addresses {
		var socks socksAddress

		// Parse into 'socks' and filter out localhost if necessary
		if strings.HasPrefix(addr, "unix:") {
			// Ignore unix ports for remote tor
			if !torOnLocalhost {
				log.Printf("Ignoring loopback SOCKS port %s", addr)
				continue
			}
			socks = socksAddress{
				Network: "unix",
				Address: addr[5:],
			}
		} else {
			ipStr, _, err := net.SplitHostPort(addr)
			if err != nil {
				log.Printf("Ignoring malformed SOCKS address '%s': %s", addr, err)
				continue
			}
			socks = socksAddress{
				Network: "tcp",
				Address: addr,
				IP:      net.ParseIP(ipStr),
			}
			if !torOnLocalhost && socks.IP.IsLoopback() {
				log.Printf("Ignoring loopback SOCKS port %s", addr)
				continue
			}
		}

		// Compare to current selection
		if socks.PreferredTo(selected, preferredIP) {
			selected = socks
		}
	}

	if !selected.IsValid() {
		return selected, errors.New("No valid SOCKS configuration")
	}

	return selected, nil
}

func (n *Network) GetProxyDialer(forward proxy.Dialer) (proxy.Dialer, error) {
	n.controlMutex.Lock()
	socks := n.socksAddress
	n.controlMutex.Unlock()

	if !socks.IsValid() {
		return nil, errors.New("No valid SOCKS configuration")
	}

	return proxy.SOCKS5(socks.Network, socks.Address, nil, forward)
}

func (n *Network) WaitForProxyDialer(forward proxy.Dialer, c context.Context) (proxy.Dialer, error) {
	var monitor <-chan interface{}
	for {
		// Check if there's a proxy address available
		n.controlMutex.Lock()
		socks := n.socksAddress
		n.controlMutex.Unlock()

		if socks.IsValid() {
			return proxy.SOCKS5(socks.Network, socks.Address, nil, forward)
		}

		if monitor == nil {
			// Subscribe to connectivity change events; this is done after the first
			// check to avoid overhead for the common case
			monitor = n.EventMonitor().Subscribe(20)
			defer n.EventMonitor().Unsubscribe(monitor)
			// Check again before blocking on the monitor now that we're subscribed,
			// in case the state changed since unlocking the mutex.
			continue
		}

		select {
		case _, ok := <-monitor:
			if !ok {
				return nil, errors.New("Event monitor closed")
			}
		case <-c.Done():
			return nil, c.Err()
		}
	}

	return nil, errors.New("No valid SOCKS configuration")
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
		Network:    n,
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
	// XXX prefer unix
	internalListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}

	onionPorts := []bulb.OnionPortSpec{
		bulb.OnionPortSpec{
			VirtPort: onionPort,
			Target:   internalListener.Addr().String(),
		},
	}

	service, err := n.AddOnionPorts(onionPorts, key)
	if err != nil {
		internalListener.Close()
		return nil, nil, err
	}

	listener := &OnionServiceListener{
		Service:          service,
		InternalListener: internalListener,
	}

	return service, listener, nil
}

func (n *Network) DeleteOnionService(onionID string) error {
	n.controlMutex.Lock()
	for i, onion := range n.onions {
		if onion.OnionID == onionID {
			n.onions = append(n.onions[:i], n.onions[i+1:]...)
			break
		}
	}
	conn := n.conn
	n.controlMutex.Unlock()

	if conn != nil {
		return conn.DeleteOnion(onionID)
	}

	return nil
}

func (s *OnionServiceListener) Accept() (net.Conn, error) {
	return s.InternalListener.Accept()
}

func (s *OnionServiceListener) Close() error {
	s.Service.Network.DeleteOnionService(s.Service.OnionID)
	return s.InternalListener.Close()
}

type OnionAddr struct {
	OnionHostname string
}

func (a OnionAddr) Network() string {
	return "onion"
}

func (a OnionAddr) String() string {
	return a.OnionHostname
}

func (s *OnionServiceListener) Addr() net.Addr {
	return OnionAddr{OnionHostname: s.Service.OnionID + ".onion"}
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

	// Choose default SOCKS port
	socks, err := chooseSocksAddress(connStatus.SocksAddress, n.controlAddress)
	if socks.IsValid() {
		log.Printf("Discovered SOCKS port %s %s", socks.Network, socks.Address)
	} else {
		log.Printf("No SOCKS port: %v", err)
	}

	n.controlMutex.Lock()

	// Copy list of onions to republish. This is done before the status
	// change to avoid racing with blocked calls to AddOnionPorts, which
	// will add to this list once the connection is available, but the
	// publication is done afterwards.
	onions := make([]*OnionService, len(n.onions))
	copy(onions, n.onions)

	// Update network status and set connection
	n.conn = conn
	n.status.Control = &ricochet.TorControlStatus{
		Status:     ricochet.TorControlStatus_CONNECTED,
		TorVersion: pinfo.TorVersion,
	}
	n.status.Connection = &connStatus
	n.socksAddress = socks
	status := n.status
	n.controlMutex.Unlock()
	n.events.Publish(status)

	// Re-publish onion services. Errors are not fatal to conn.
	publishOnions(conn, onions)

	return nil
}

func createConnection(address, password string) (*bulb.Conn, error) {
	var net, addr string
	if strings.HasPrefix(address, "unix:") {
		net = "unix"
		addr = address[5:]
	} else {
		net = "tcp"
		addr = address
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

	log.Printf("Control connection to %s successful", address)
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
