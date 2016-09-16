package main

import (
	"fmt"
	"github.com/special/notricochet/rpc"
	"golang.org/x/net/context"
	"gopkg.in/readline.v1"
	"log"
)

type Client struct {
	Backend ricochet.RicochetCoreClient
	Input   *readline.Instance

	ServerStatus ricochet.ServerStatusReply
	Identity     ricochet.Identity

	// XXX threadsafety
	NetworkStatus ricochet.NetworkStatus
	Contacts      []*ricochet.Contact
}

// XXX need to handle backend connection loss/reconnection..
func (c *Client) Initialize() error {
	// Query server status and version
	status, err := c.Backend.GetServerStatus(context.Background(), &ricochet.ServerStatusRequest{
		RpcVersion: 1,
	})
	if err != nil {
		return err
	}
	c.ServerStatus = *status
	if status.RpcVersion != 1 {
		return fmt.Errorf("Unsupported backend RPC version %d", status.RpcVersion)
	}

	// Query identity
	identity, err := c.Backend.GetIdentity(context.Background(), &ricochet.IdentityRequest{})
	if err != nil {
		return err
	}
	c.Identity = *identity

	// Spawn routines to query and monitor state changes
	go c.monitorNetwork()
	go c.monitorContacts()

	// XXX block until populated/initialized?
	return nil
}

func (c *Client) monitorNetwork() {
	stream, err := c.Backend.MonitorNetwork(context.Background(), &ricochet.MonitorNetworkRequest{})
	if err != nil {
		log.Printf("Initializing network status monitor failed: %v", err)
		// XXX handle
		return
	}

	for {
		status, err := stream.Recv()
		if err != nil {
			log.Printf("Network status monitor error: %v", err)
			// XXX handle
			break
		}

		log.Printf("Network status changed: %v", status)
		c.NetworkStatus = *status
	}
}

func (c *Client) monitorContacts() {
	stream, err := c.Backend.MonitorContacts(context.Background(), &ricochet.MonitorContactsRequest{})
	if err != nil {
		log.Printf("Initializing contact status monitor failed: %v", err)
		// XXX handle
		return
	}

	// Populate initial contacts list
	for {
		event, err := stream.Recv()
		if err != nil {
			log.Printf("Contact populate error: %v", err)
			// XXX handle
			break
		}

		if event.Type != ricochet.ContactEvent_POPULATE {
			log.Printf("Ignoring unexpected contact event during populate: %v", event)
			continue
		}

		// Populate is terminated by a nil subject
		if event.Subject == nil {
			break
		}

		if contact := event.GetContact(); contact != nil {
			c.Contacts = append(c.Contacts, contact)
		} else if request := event.GetRequest(); request != nil {
			// XXX handle requests
			log.Printf("XXX contact requests not supported")
		} else {
			log.Printf("XXX invalid event")
		}
	}

	log.Printf("Loaded %d contacts", len(c.Contacts))

	for {
		event, err := stream.Recv()
		if err != nil {
			log.Printf("Contact status monitor error: %v", err)
			// XXX handle
			break
		}

		contact := event.GetContact()

		switch event.Type {
		case ricochet.ContactEvent_ADD:
			if contact == nil {
				log.Printf("Ignoring contact add event with null contact")
				continue
			}
			c.Contacts = append(c.Contacts, contact)
			log.Printf("new contact: %v", contact)

		case ricochet.ContactEvent_UPDATE:
			if contact == nil {
				log.Printf("Ignoring contact update event with null contact")
				continue
			}

			var found bool
			for i, match := range c.Contacts {
				if match.Id == contact.Id && match.Address == contact.Address {
					contacts := append(c.Contacts[0:i], contact)
					contacts = append(contacts, c.Contacts[i+1:]...)
					c.Contacts = contacts
					found = true
					break
				}
			}

			if !found {
				log.Printf("Ignoring contact update event for unknown contact: %v", contact)
			} else {
				log.Printf("updated contact: %v", contact)
			}

		case ricochet.ContactEvent_DELETE:
			if contact == nil {
				log.Printf("Ignoring contact delete event with null contact")
				continue
			}

			var found bool
			for i, match := range c.Contacts {
				if match.Id == contact.Id && match.Address == contact.Address {
					c.Contacts = append(c.Contacts[0:i], c.Contacts[i+1:]...)
					found = true
					break
				}
			}

			if !found {
				log.Printf("Ignoring contact delete event for unknown contact: %v", contact)
			} else {
				log.Printf("deleted contact: %v", contact)
			}

		default:
			log.Printf("Ignoring unknown contact event: %v", event)
		}
	}
}
