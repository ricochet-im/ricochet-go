package core

import (
	"fmt"
	protocol "github.com/s-rah/go-ricochet"
	"github.com/special/notricochet/rpc"
	"log"
	"strings"
	"sync"
	"time"
)

// XXX There is generally a lot of duplication and boilerplate between
// Contact, ConfigContact, and rpc.Contact. This should be reduced somehow.

type Contact struct {
	id   int
	data ConfigContact

	mutex sync.Mutex

	connection *protocol.OpenConnection
}

func ContactFromConfig(id int, data ConfigContact) (*Contact, error) {
	contact := &Contact{
		id:   id,
		data: data,
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
	if c.connection == nil {
		return ricochet.Contact_UNKNOWN
	} else {
		return ricochet.Contact_ONLINE
	}
}

func (c *Contact) SetConnection(conn *protocol.OpenConnection) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if conn == c.connection {
		return fmt.Errorf("Duplicate assignment of connection %v to contact %v", conn, c)
	}

	if !conn.IsAuthed || conn.Closed {
		return fmt.Errorf("Connection %v is not in a valid state to assign to contact %v", conn, c)
	}

	if c.data.Hostname[0:16] != conn.OtherHostname {
		return fmt.Errorf("Connection hostname %s doesn't match contact hostname %s when assigning connection", conn.OtherHostname, c.data.Hostname[0:16])
	}

	if c.connection != nil && c.connection.Closed {
		log.Printf("Replacing dead connection %v for contact %v", c.connection, c)
		c.connection = nil
	}

	// Decide whether to replace an existing connection with this one
	if c.connection != nil {
		// If the existing connection is in the same direction, always use the new one
		if c.connection.Client == conn.Client {
			log.Printf("Replacing existing same-direction connection %v with new connection %v for contact %v", c.connection, conn, c)
			c.connection.Close()
			c.connection = nil
		}

		// If the existing connection is more than 30 seconds old, use the new one
		// XXX implement this

		// Fall back to string comparison of hostnames for a stable resolution
		preferOutbound := conn.MyHostname < conn.OtherHostname
		if preferOutbound == conn.Client {
			// New connection wins
			log.Printf("Replacing existing connection %v with new connection %v for contact %v according to fallback order", c.connection, conn, c)
			c.connection.Close()
			c.connection = nil
		} else {
			// Old connection wins
			log.Printf("Keeping existing connection %v instead of new connection %v for contact %v according to fallback order", c.connection, conn, c)
			conn.Close()
			return fmt.Errorf("Using existing connection")
		}
	}

	// If this connection is inbound and there's an outbound attempt, keep this
	// connection and cancel outbound if we haven't sent authentication yet, or
	// if the outbound connection will lose the fallback comparison above.
	// XXX implement this

	c.connection = conn
	log.Printf("Assigned connection %v to contact %v", c.connection, c)

	// XXX implicit accept contact requests
	// XXX update connected date
	// XXX signal state and data changes
	// XXX react to connection state changes

	return nil
}
