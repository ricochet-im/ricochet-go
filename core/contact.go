package core

import (
	"fmt"
	"strings"
	"time"
)

// XXX There is generally a lot of duplication and boilerplate between
// Contact, ConfigContact, and rpc.Contact. This should be reduced somehow.

// XXX This is threadsafe only because it can't be modified right now.

type Contact struct {
	id int

	data ConfigContact
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
	return c.data.Nickname
}

func (c *Contact) Address() string {
	return "ricochet:" + c.data.Hostname[0:16]
}

func (c *Contact) Hostname() string {
	return c.data.Hostname
}

func (c *Contact) LastConnected() time.Time {
	time, _ := time.Parse(time.RFC3339, c.data.LastConnected)
	return time
}

func (c *Contact) WhenCreated() time.Time {
	time, _ := time.Parse(time.RFC3339, c.data.WhenCreated)
	return time
}
