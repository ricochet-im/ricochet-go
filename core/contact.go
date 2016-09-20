package core

import (
	"fmt"
	protocol "github.com/s-rah/go-ricochet"
	"github.com/special/notricochet/core/utils"
	"github.com/special/notricochet/rpc"
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

	connection *protocol.OpenConnection
}

func ContactFromConfig(core *Ricochet, id int, data ConfigContact, events *utils.Publisher) (*Contact, error) {
	contact := &Contact{
		core:   core,
		id:     id,
		data:   data,
		events: events,
	}

	if id < 0 {
		return nil, fmt.Errorf("Invalid contact ID '%d'", id)
	} else if len(data.Hostname) != 22 || !strings.HasSuffix(data.Hostname, ".onion") {
		return nil, fmt.Errorf("Invalid contact hostname '%s", data.Hostname)
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

func (c *Contact) SetConnection(conn *protocol.OpenConnection) error {
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

	// XXX react to connection state changes
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

	return nil
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

func (c *Contact) OnConnectionClosed(conn *protocol.OpenConnection) {
	c.mutex.Lock()

	if c.connection != conn {
		c.mutex.Unlock()
		return
	}

	c.connection = nil
	c.status = ricochet.Contact_OFFLINE

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
}
