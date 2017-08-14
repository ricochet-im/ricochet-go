package core

import (
	"fmt"
	"github.com/ricochet-im/ricochet-go/core/utils"
	"github.com/ricochet-im/ricochet-go/rpc"
	protocol "github.com/s-rah/go-ricochet"
	connection "github.com/s-rah/go-ricochet/connection"
	"golang.org/x/net/context"
	"log"
	"strconv"
	"sync"
	"time"
)

// XXX There is generally a lot of duplication and boilerplate between
// Contact, ConfigContact, and rpc.Contact. This should be reduced somehow.

// XXX Consider replacing the config contact with the protobuf structure,
// and extending the protobuf structure for everything it needs.

type Contact struct {
	core *Ricochet

	id     int
	data   ConfigContact
	status ricochet.Contact_Status

	mutex  sync.Mutex
	events *utils.Publisher

	connEnabled       bool
	connection        *connection.Connection
	connChannel       chan *connection.Connection
	connEnabledSignal chan bool
	connectionOnce    sync.Once

	timeConnected time.Time

	conversation *Conversation
}

func ContactFromConfig(core *Ricochet, id int, data ConfigContact, events *utils.Publisher) (*Contact, error) {
	contact := &Contact{
		core:              core,
		id:                id,
		data:              data,
		events:            events,
		connChannel:       make(chan *connection.Connection),
		connEnabledSignal: make(chan bool),
	}

	if id < 0 {
		return nil, fmt.Errorf("Invalid contact ID '%d'", id)
	} else if !IsOnionValid(data.Hostname) {
		return nil, fmt.Errorf("Invalid contact hostname '%s", data.Hostname)
	}

	if data.Request.Pending {
		if data.Request.WhenRejected != "" {
			contact.status = ricochet.Contact_REJECTED
		} else {
			contact.status = ricochet.Contact_REQUEST
		}
	}

	return contact, nil
}

func (c *Contact) Id() int {
	return c.id
}

func (c *Contact) Nickname() string {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.data.Nickname
}

func (c *Contact) Address() string {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	address, _ := AddressFromOnion(c.data.Hostname)
	return address
}

func (c *Contact) Hostname() string {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.data.Hostname
}

func (c *Contact) LastConnected() time.Time {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	time, _ := time.Parse(time.RFC3339, c.data.LastConnected)
	return time
}

func (c *Contact) WhenCreated() time.Time {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	time, _ := time.Parse(time.RFC3339, c.data.WhenCreated)
	return time
}

func (c *Contact) Status() ricochet.Contact_Status {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.status
}

func (c *Contact) Data() *ricochet.Contact {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	address, _ := AddressFromOnion(c.data.Hostname)
	data := &ricochet.Contact{
		Id:            int32(c.id),
		Address:       address,
		Nickname:      c.data.Nickname,
		WhenCreated:   c.data.WhenCreated,
		LastConnected: c.data.LastConnected,
		Status:        c.status,
	}
	if c.data.Request.Pending {
		data.Request = &ricochet.ContactRequest{
			Direction:    ricochet.ContactRequest_OUTBOUND,
			Address:      data.Address,
			Nickname:     data.Nickname,
			Text:         c.data.Request.Message,
			FromNickname: c.data.Request.MyNickname,
		}
	}
	return data
}

func (c *Contact) IsRequest() bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.data.Request.Pending
}

func (c *Contact) Conversation() *Conversation {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.conversation == nil {
		address, _ := AddressFromOnion(c.data.Hostname)
		entity := &ricochet.Entity{
			ContactId: int32(c.id),
			Address:   address,
		}
		c.conversation = NewConversation(c, entity, c.core.Identity.ConversationStream)
	}
	return c.conversation
}

func (c *Contact) Connection() *connection.Connection {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.connection
}

// StartConnection enables inbound and outbound connections for this contact, if other
// conditions permit them. This function is safe to call repeatedly.
func (c *Contact) StartConnection() {
	c.connectionOnce.Do(func() {
		go c.contactConnection()
	})

	c.connEnabled = true
	c.connEnabledSignal <- true
}

func (c *Contact) StopConnection() {
	// Must be running to consume connEnabledSignal
	c.connectionOnce.Do(func() {
		go c.contactConnection()
	})

	c.connEnabled = false
	c.connEnabledSignal <- false
}

func (c *Contact) shouldMakeOutboundConnections() bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Don't make connections to contacts in the REJECTED state
	if c.status == ricochet.Contact_REJECTED {
		return false
	}

	return c.connEnabled
}

// closeUnhandledConnection takes a connection without an active Process routine
// and ensures that it is fully closed and destroyed. It is safe to call on
// a connection that has already been closed and on any connection in any
// state, as long as Process() is not currently running.
func closeUnhandledConnection(conn *connection.Connection) {
	conn.Conn.Close()
	nullHandler := &connection.AutoConnectionHandler{}
	nullHandler.Init()
	conn.Process(nullHandler)
}

// Goroutine to handle the protocol connection for a contact.
// Responsible for making outbound connections and taking over authenticated
// inbound connections, running protocol handlers on the active connection, and
// reacting to connection loss. Nothing else may write Contact.connection.
//
// This goroutine is started by the first call to StartConnection or StopConnection
// and persists for the lifetime of the contact. When connections are stopped, it
// consumes connChannel and closes all (presumably inbound) connections.
// XXX Need a hard kill for destroying contacts
func (c *Contact) contactConnection() {
	// Signalled when the active connection is closed
	connClosedChannel := make(chan struct{})
	connectionsEnabled := false

	for {
		if !connectionsEnabled {
			// Reject all connections on connChannel and wait for start signal
			select {
			case conn := <-c.connChannel:
				if conn != nil {
					log.Printf("Discarded connection to %s because connections are disabled", c.Address())
					go closeUnhandledConnection(conn)
					// XXX-protocol doing this here instead of during auth means they'll keep trying endlessly. Doing it in
					// auth means they'll never try again. Both are sometimes wrong. Hmm.
				}
			case enable := <-c.connEnabledSignal:
				if enable {
					log.Printf("Contact %s connections are enabled", c.Address())
					connectionsEnabled = true
				}
				// XXX hard kill
			}
			continue
		}

		// If there is no active connection, spawn an outbound connector. A successful connection
		// is returned via connChannel, and otherwise it will keep trying until cancelled via
		// the context.
		var outboundCtx context.Context
		outboundCancel := func() {}
		if c.connection == nil && c.shouldMakeOutboundConnections() {
			outboundCtx, outboundCancel = context.WithCancel(context.Background())
			go c.connectOutbound(outboundCtx, c.connChannel)
		}

		select {
		case conn := <-c.connChannel:
			outboundCancel()
			if conn == nil {
				// Signal used to restart outbound connection attempts
				continue
			}

			c.mutex.Lock()
			// Decide whether to keep this connection; if this returns an error, conn is
			// already closed. If there was an existing connection and this returns nil,
			// the old connection is closed but c.connection has not been reset.
			if err := c.considerUsingConnection(conn); err != nil {
				log.Printf("Discarded new contact %s connection: %s", c.data.Hostname, err)
				go closeUnhandledConnection(conn)
				c.mutex.Unlock()
				continue
			}
			replacingConn := c.connection != nil
			c.connection = conn
			if replacingConn {
				// Wait for old handleConnection to return
				c.mutex.Unlock()
				<-connClosedChannel
				c.mutex.Lock()
			}
			go c.handleConnection(conn, connClosedChannel)
			c.onConnectionStateChanged()
			c.mutex.Unlock()

		case <-connClosedChannel:
			outboundCancel()
			c.mutex.Lock()
			c.connection = nil
			c.onConnectionStateChanged()
			c.mutex.Unlock()

		case enable := <-c.connEnabledSignal:
			outboundCancel()
			if !enable {
				connectionsEnabled = false
				log.Printf("Contact %s connections are disabled", c.Address())
			}
		}
	}

	log.Printf("Exiting contact connection loop for %s", c.Address())
	c.mutex.Lock()
	if c.connection != nil {
		c.connection.Conn.Close()
		c.connection = nil
		c.onConnectionStateChanged()
		c.mutex.Unlock()
		<-connClosedChannel
	} else {
		c.mutex.Unlock()
	}
}

// Goroutine to maintain an open contact connection, calls Process and reports when closed.
func (c *Contact) handleConnection(conn *connection.Connection, closedChannel chan struct{}) {
	// Connection does not outlive this function
	defer func() {
		conn.Conn.Close()
		closedChannel <- struct{}{}
	}()
	log.Printf("Contact connection for %s ready", conn.RemoteHostname)
	handler := NewContactProtocolHandler(c, conn)
	err := conn.Process(handler)
	if err == nil {
		// Somebody called Break?
		err = fmt.Errorf("Connection handler interrupted unexpectedly")
	}
	log.Printf("Contact connection for %s closed: %s", conn.RemoteHostname, err)
}

// Attempt an outbound connection to the contact, retrying automatically using OnionConnector.
// This function _must_ send something to connChannel before returning, unless the context has
// been cancelled.
func (c *Contact) connectOutbound(ctx context.Context, connChannel chan *connection.Connection) {
	c.mutex.Lock()
	connector := OnionConnector{
		Network:     c.core.Network,
		NeverGiveUp: true,
	}
	hostname := c.data.Hostname
	isRequest := c.data.Request.Pending
	c.mutex.Unlock()

	for {
		conn, err := connector.Connect(hostname+":9878", ctx)
		if err != nil {
			// The only failure here should be context, because NeverGiveUp
			// is set, but be robust anyway.
			if ctx.Err() != nil {
				return
			}

			log.Printf("Contact connection failure: %s", err)
			continue
		}

		// XXX-protocol Ideally this should all take place under ctx also; easy option is a goroutine
		// blocked on ctx that kills the connection.
		log.Printf("Successful outbound connection to contact %s", hostname)
		oc, err := protocol.NegotiateVersionOutbound(conn, hostname[0:16])
		if err != nil {
			log.Printf("Outbound connection version negotiation failed: %v", err)
			conn.Close()
			if err := connector.Backoff(ctx); err != nil {
				return
			}
			continue
		}

		log.Printf("Outbound connection negotiated version; authenticating")
		privateKey := c.core.Identity.PrivateKey()
		known, err := connection.HandleOutboundConnection(oc).ProcessAuthAsClient(&privateKey)
		if err != nil {
			log.Printf("Outbound connection authentication failed: %v", err)
			closeUnhandledConnection(oc)
			if err := connector.Backoff(ctx); err != nil {
				return
			}
			continue
		}

		// XXX-protocol Also move the "this is an outbound request" logic here to kill the silly flag and
		//     move those out of a scary mutexed path? XXX
		if !known && !isRequest {
			log.Printf("Outbound connection to contact says we are not a known contact for %v", c)
			// XXX Should move to rejected status, stop attempting connections.
			closeUnhandledConnection(oc)
			if err := connector.Backoff(ctx); err != nil {
				return
			}
			continue
		} else if known && isRequest {
			log.Printf("Contact request implicitly accepted for outbound connection by contact %v", c)
			c.UpdateContactRequest("Accepted")
		}

		log.Printf("Assigning outbound connection to contact")
		c.AssignConnection(oc)
		break
	}
}

// considerUsingConnection takes a newly established connection and decides whether
// the new connection is valid and acceptable, and whether to replace or keep an
// existing connection. To handle race cases when peers are connecting to eachother,
// a particular set of rules is followed for replacing an existing connection.
//
// considerUsingConnection returns nil if the new connection is valid and should be
// used. If this function returns nil, the existing connection has been closed (but
// c.connection is unmodified, and the process routine may still be executing). If
// this function returns an error, conn has been closed.
//
// Assumes that c.mutex is held.
func (c *Contact) considerUsingConnection(conn *connection.Connection) error {
	killConn := conn
	defer func() {
		if killConn != nil {
			killConn.Conn.Close()
		}
	}()

	if conn.IsInbound {
		log.Printf("Contact %s has a new inbound connection", c.data.Hostname)
	} else {
		log.Printf("Contact %s has a new outbound connection", c.data.Hostname)
	}

	if conn == c.connection {
		return fmt.Errorf("Duplicate assignment of connection %v to contact %v", conn, c)
	}

	if !conn.Authentication["im.ricochet.auth.hidden-service"] {
		return fmt.Errorf("Connection %v is not authenticated", conn)
	}

	if c.data.Hostname[0:16] != conn.RemoteHostname {
		return fmt.Errorf("Connection hostname %s doesn't match contact hostname %s when assigning connection", conn.RemoteHostname, c.data.Hostname[0:16])
	}

	if c.connection != nil && !c.shouldReplaceConnection(conn) {
		return fmt.Errorf("Using existing connection")
	}

	// If this connection is inbound and there's an outbound attempt, keep this
	// connection and cancel outbound if we haven't sent authentication yet, or
	// if the outbound connection will lose the fallback comparison above.
	// XXX implement this; currently outbound is always cancelled when an inbound
	// connection succeeds.

	// We will keep conn, close c.connection instead if there was one
	killConn = c.connection
	return nil
}

// onConnectionStateChanged is called by the connection loop when the c.connection
// is changed, which can be a transition to online or offline or a replacement.
// Assumes c.mutex is held.
func (c *Contact) onConnectionStateChanged() {
	if c.connection != nil {
		if c.data.Request.Pending {
			if !c.connection.IsInbound {
				// Outbound connection for contact request; send request message
				log.Printf("Sending outbound contact request to %v", c)
				// XXX-protocol ooooohhhhh no you don't. Cannot interact w/ protocol here, because process may
				// not have started yet. Maybe this one needs to go w/ outbound auth in pre-connection also
				// honestly, it would not be a bad thing to have outbound unaccepted requests _not_ be
				// considered active connections, as long as they get handled properly.
				// c.connection.SendContactRequest(5, c.data.Request.MyNickname, c.data.Request.Message)
			} else {
				// Inbound connection implicitly accepts the contact request and can continue as a contact
				log.Printf("Contact request implicitly accepted by contact %v", c)
				c.updateContactRequest("Accepted")
			}
		} else {
			c.status = ricochet.Contact_ONLINE
		}
	} else {
		if c.status == ricochet.Contact_ONLINE {
			c.status = ricochet.Contact_OFFLINE
		}
	}

	// Update LastConnected time
	c.timeConnected = time.Now()

	config := c.core.Config.OpenWrite()
	c.data.LastConnected = c.timeConnected.Format(time.RFC3339)
	config.Contacts[strconv.Itoa(c.id)] = c.data
	config.Save()

	// _really_ assumes c.mutex was held
	c.mutex.Unlock()
	event := ricochet.ContactEvent{
		Type: ricochet.ContactEvent_UPDATE,
		Subject: &ricochet.ContactEvent_Contact{
			Contact: c.Data(),
		},
	}
	c.events.Publish(event)

	if c.connection != nil {
		// Send any queued messages
		sent := c.Conversation().SendQueuedMessages()
		if sent > 0 {
			log.Printf("Sent %d queued messages to contact", sent)
		}
	}

	c.mutex.Lock()
}

// Decide whether to replace the existing connection with conn.
// Assumes mutex is held.
func (c *Contact) shouldReplaceConnection(conn *connection.Connection) bool {
	myHostname, _ := PlainHostFromAddress(c.core.Identity.Address())
	if c.connection == nil {
		return true
	} else if c.connection.IsInbound == conn.IsInbound {
		// If the existing connection is in the same direction, always use the new one
		log.Printf("Replacing existing same-direction connection %v with new connection %v for contact %v", c.connection, conn, c)
		return true
	} else if time.Since(c.timeConnected) > (30 * time.Second) {
		// If the existing connection is more than 30 seconds old, use the new one
		log.Printf("Replacing existing %v old connection %v with new connection %v for contact %v", time.Since(c.timeConnected), c.connection, conn, c)
		return true
	} else if preferOutbound := myHostname < conn.RemoteHostname; preferOutbound != conn.IsInbound {
		// Fall back to string comparison of hostnames for a stable resolution
		// New connection wins
		log.Printf("Replacing existing connection %v with new connection %v for contact %v according to fallback order", c.connection, conn, c)
		return true
	} else {
		// Old connection wins fallback
		log.Printf("Keeping existing connection %v instead of new connection %v for contact %v according to fallback order", c.connection, conn, c)
		return false
	}
	return false
}

// Update the status of a contact request from a protocol event. Returns
// true if the contact request channel should remain open.
func (c *Contact) UpdateContactRequest(status string) bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !c.data.Request.Pending {
		return false
	}

	re := c.updateContactRequest(status)

	event := ricochet.ContactEvent{
		Type: ricochet.ContactEvent_UPDATE,
		Subject: &ricochet.ContactEvent_Contact{
			Contact: c.Data(),
		},
	}
	c.events.Publish(event)

	return re
}

// Same as above, but assumes the mutex is already held and that the caller
// will send an UPDATE event
func (c *Contact) updateContactRequest(status string) bool {
	config := c.core.Config.OpenWrite()
	now := time.Now().Format(time.RFC3339)
	// Whether to keep the channel open
	var re bool

	switch status {
	case "Pending":
		c.data.Request.WhenDelivered = now
		re = true

	case "Accepted":
		c.data.Request = ConfigContactRequest{}
		if c.connection != nil {
			c.status = ricochet.Contact_ONLINE
		} else {
			c.status = ricochet.Contact_UNKNOWN
		}

	case "Rejected":
		c.data.Request.WhenRejected = now

	case "Error":
		c.data.Request.WhenRejected = now
		c.data.Request.RemoteError = "error occurred"

	default:
		log.Printf("Unknown contact request status '%s'", status)
	}

	config.Contacts[strconv.Itoa(c.id)] = c.data
	config.Save()
	return re
}

// AssignConnection takes new connections, inbound or outbound, to this contact, and
// asynchronously decides whether to keep or close them.
func (c *Contact) AssignConnection(conn *connection.Connection) {
	c.connectionOnce.Do(func() {
		go c.contactConnection()
	})

	// If connections are disabled, this connection will be closed by contactConnection
	c.connChannel <- conn
}
