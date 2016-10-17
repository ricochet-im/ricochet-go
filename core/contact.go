package core

import (
	"fmt"
	protocol "github.com/s-rah/go-ricochet"
	"github.com/ricochet-im/ricochet-go/core/utils"
	"github.com/ricochet-im/ricochet-go/rpc"
	"golang.org/x/net/context"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

// XXX There is generally a lot of duplication and boilerplate between
// Contact, ConfigContact, and rpc.Contact. This should be reduced somehow.

type Contact struct {
	core *Ricochet

	id     int
	data   ConfigContact
	status ricochet.Contact_Status

	mutex  sync.Mutex
	events *utils.Publisher

	connection        *protocol.OpenConnection
	connChannel       chan *protocol.OpenConnection
	connClosedChannel chan struct{}

	conversation *Conversation
}

func ContactFromConfig(core *Ricochet, id int, data ConfigContact, events *utils.Publisher) (*Contact, error) {
	contact := &Contact{
		core:              core,
		id:                id,
		data:              data,
		events:            events,
		connChannel:       make(chan *protocol.OpenConnection),
		connClosedChannel: make(chan struct{}),
	}

	if id < 0 {
		return nil, fmt.Errorf("Invalid contact ID '%d'", id)
	} else if len(data.Hostname) != 22 || !strings.HasSuffix(data.Hostname, ".onion") {
		return nil, fmt.Errorf("Invalid contact hostname '%s", data.Hostname)
	}

	// XXX Should have some global trigger that starts all contact connections
	// at the right time
	go contact.contactConnection()

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
	return "ricochet:" + c.data.Hostname[0:16]
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
	data := &ricochet.Contact{
		Id:            int32(c.id),
		Address:       "ricochet:" + c.data.Hostname[0:16],
		Nickname:      c.data.Nickname,
		WhenCreated:   c.data.WhenCreated,
		LastConnected: c.data.LastConnected,
		Status:        c.status,
	}
	return data
}

func (c *Contact) Conversation() *Conversation {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.conversation == nil {
		entity := &ricochet.Entity{
			ContactId: int32(c.id),
			Address:   "ricochet:" + c.data.Hostname[0:16],
		}
		c.conversation = NewConversation(c, entity, c.core.Identity.ConversationStream)
	}
	return c.conversation
}

// XXX Thread safety disaster
func (c *Contact) Connection() *protocol.OpenConnection {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.connection
}

// Goroutine to handle the protocol connection for a contact.
// Responsible for making outbound connections and taking over authenticated
// inbound connections, running protocol handlers on the active connection, and
// reacting to connection loss. Nothing else may write Contact.connection.
func (c *Contact) contactConnection() {
	// XXX Should the protocol continue handling its own goroutines?
	// I'm thinking the protocol API design I want is:
	//   - "handler" assigned per-connection
	//   - each inbound listener has its own handler also, assigns for conns
	//   - so, non-authed connection has a handler that _can't_ do anything other than auth
	//   - inbound contact req connection gets a special handler for that case
	//   - authenticated contact conns get handler changed here
	// It's probably more sensible to never break the conn read loop, because of buffering etc
	// So likely we don't want to change that goroutine. Could still use a channel to pass it
	// to handler for parsing, which could let it go on any goroutine we want, if it's desirable
	// to put it on e.g. this routine. Hmm.

	for {
		// If there is no active connection, spawn an outbound connector.
		// A successful connection is returned via connChannel; otherwise, it will keep trying.
		var outboundCtx context.Context
		outboundCancel := func() {}
		if c.connection == nil {
			outboundCtx, outboundCancel = context.WithCancel(context.Background())
			go c.connectOutbound(outboundCtx, c.connChannel)
		}

		select {
		case conn, ok := <-c.connChannel:
			outboundCancel()
			if !ok {
				// Closing connChannel exits this connection routine, for contact
				// deletion, exit, or some other case.
				break
			} else if conn == nil {
				// Signal used to restart outbound connection attempts
				continue
			}

			// Received a new connection. The connection is already authenticated and ready to use.
			// This connection may be inbound or outbound. setConnection will decide whether to
			// replace an existing connection.

			// XXX Tweak setConnection logic if needed
			// Logic when keeping connection, need to make sure protocol is going, etc...
			if err := c.setConnection(conn); err != nil {
				log.Printf("Discarded new contact %s connection: %s", c.Address(), err)
				if !conn.Closed && conn != c.connection {
					conn.Close()
				}
				continue
			}

		case <-c.connClosedChannel:
			outboundCancel()
			c.clearConnection(nil)
		}
	}

	log.Printf("Exiting contact connection loop for %s", c.Address())
	c.clearConnection(nil)
}

// Attempt an outbound connection to the contact, retrying automatically using OnionConnector.
// This function _must_ send something to connChannel before returning, unless the context has
// been cancelled.
func (c *Contact) connectOutbound(ctx context.Context, connChannel chan *protocol.OpenConnection) {
	c.mutex.Lock()
	connector := OnionConnector{
		Network:     c.core.Network,
		NeverGiveUp: true,
	}
	hostname := c.data.Hostname
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

		log.Printf("Successful outbound connection to contact %s", hostname)
		oc, err := protocol.Open(conn, hostname[0:16])
		if err != nil {
			log.Printf("Contact connection protocol failure: %s", err)
			oc.Close()
			// XXX These failures are probably not worth retrying so much,
			// but that would need to be investigated. For now, just do the
			// same backoff behavior.
			if err := connector.Backoff(ctx); err != nil {
				return
			}
			continue
		} else {
			log.Printf("Protocol connection open: %v", oc)
			// XXX Protocol API needs to be reworked; see notes in
			// contactConnection. Ideally we should authenticate here and
			// pass the conneciton back, but for now this can do nothing:
			// the connection will either succeed and come in via the
			// protocol handler, or will be closed and signalled via
			// OnConnectionClosed. Alternatively, it will break because this
			// is fragile and dumb.
			// XXX BUG: This means no backoff for authentication failure
			handler := &ProtocolConnection{
				Conn:       oc,
				Contact:    c,
				MyHostname: c.core.Identity.Address()[9:],
				PrivateKey: c.core.Identity.PrivateKey(),
			}
			go oc.Process(handler)
			return
		}
	}
}

func (c *Contact) setConnection(conn *protocol.OpenConnection) error {
	if conn.Client {
		log.Printf("Contact %s has a new outbound connection", c.Address())
	} else {
		log.Printf("Contact %s has a new inbound connection", c.Address())
	}

	c.mutex.Lock()

	if conn == c.connection {
		c.mutex.Unlock()
		return fmt.Errorf("Duplicate assignment of connection %v to contact %v", conn, c)
	}

	if !conn.IsAuthed || conn.Closed {
		c.mutex.Unlock()
		conn.Close()
		return fmt.Errorf("Connection %v is not in a valid state to assign to contact %v", conn, c)
	}

	if c.data.Hostname[0:16] != conn.OtherHostname {
		c.mutex.Unlock()
		conn.Close()
		return fmt.Errorf("Connection hostname %s doesn't match contact hostname %s when assigning connection", conn.OtherHostname, c.data.Hostname[0:16])
	}

	if c.connection != nil {
		if c.shouldReplaceConnection(conn) {
			// XXX Signal state change for connection loss?
			c.connection.Close()
			c.connection = nil
		} else {
			c.mutex.Unlock()
			conn.Close()
			return fmt.Errorf("Using existing connection")
		}
	}

	// If this connection is inbound and there's an outbound attempt, keep this
	// connection and cancel outbound if we haven't sent authentication yet, or
	// if the outbound connection will lose the fallback comparison above.
	// XXX implement this

	c.connection = conn
	c.status = ricochet.Contact_ONLINE
	log.Printf("Assigned connection %v to contact %v", c.connection, c)

	// XXX implicit accept contact requests

	// Update LastConnected time
	config := c.core.Config.OpenWrite()
	c.data.LastConnected = time.Now().Format(time.RFC3339)
	config.Contacts[strconv.Itoa(c.id)] = c.data
	config.Save()

	c.mutex.Unlock()

	event := ricochet.ContactEvent{
		Type: ricochet.ContactEvent_UPDATE,
		Subject: &ricochet.ContactEvent_Contact{
			Contact: c.Data(),
		},
	}
	c.events.Publish(event)

	// Send any queued messages
	sent := c.Conversation().SendQueuedMessages()
	if sent > 0 {
		log.Printf("Sent %d queued messages to contact", sent)
	}

	return nil
}

// Close and clear state related to the active contact connection.
// If ifConn is non-nil, the active connection is only cleared if
// it is the same as ifConn. Returns true if cleared.
func (c *Contact) clearConnection(ifConn *protocol.OpenConnection) bool {
	c.mutex.Lock()
	if c.connection == nil || (ifConn != nil && c.connection != ifConn) {
		c.mutex.Unlock()
		return false
	}

	conn := c.connection
	c.connection = nil
	c.status = ricochet.Contact_OFFLINE

	// XXX eww, and also potentially deadlockable?
	config := c.core.Config.OpenWrite()
	c.data.LastConnected = time.Now().Format(time.RFC3339)
	config.Contacts[strconv.Itoa(c.id)] = c.data
	config.Save()
	c.mutex.Unlock()

	if conn != nil && !conn.Closed {
		conn.Close()
	}

	event := ricochet.ContactEvent{
		Type: ricochet.ContactEvent_UPDATE,
		Subject: &ricochet.ContactEvent_Contact{
			Contact: c.Data(),
		},
	}
	c.events.Publish(event)
	return true
}

// Decide whether to replace the existing connection with conn.
// Assumes mutex is held.
func (c *Contact) shouldReplaceConnection(conn *protocol.OpenConnection) bool {
	if c.connection == nil {
		return true
	} else if c.connection.Closed {
		log.Printf("Replacing dead connection %v for contact %v", c.connection, c)
		return true
	} else if c.connection.Client == conn.Client {
		// If the existing connection is in the same direction, always use the new one
		log.Printf("Replacing existing same-direction connection %v with new connection %v for contact %v", c.connection, conn, c)
		return true
	} else if false {
		// If the existing connection is more than 30 seconds old, use the new one
		// XXX implement this
	} else if preferOutbound := conn.MyHostname < conn.OtherHostname; preferOutbound == conn.Client {
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

// XXX also will go away during protocol API rework
func (c *Contact) OnConnectionAuthenticated(conn *protocol.OpenConnection) {
	c.connChannel <- conn
}

// XXX rework connection close to have a proper notification instead of this "find contact" mess.
func (c *Contact) OnConnectionClosed(conn *protocol.OpenConnection) {
	c.mutex.Lock()
	if c.connection != conn {
		c.mutex.Unlock()
		return
	}
	c.mutex.Unlock()
	c.connClosedChannel <- struct{}{}
}
