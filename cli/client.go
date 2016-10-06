package main

import (
	"fmt"
	"github.com/special/notricochet/rpc"
	"golang.org/x/net/context"
	"gopkg.in/readline.v1"
	"log"
	"strings"
)

type Client struct {
	Backend ricochet.RicochetCoreClient
	Input   *readline.Instance

	ServerStatus ricochet.ServerStatusReply
	Identity     ricochet.Identity

	// XXX threadsafety
	NetworkStatus ricochet.NetworkStatus
	Contacts      []*ricochet.Contact

	CurrentContact *ricochet.Contact
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
	go c.monitorConversations()

	// XXX block until populated/initialized?
	return nil
}

func (c *Client) SetCurrentContact(contact *ricochet.Contact) {
	c.CurrentContact = contact
	if c.CurrentContact != nil {
		config := *c.Input.Config
		config.Prompt = fmt.Sprintf("%s > ", c.CurrentContact.Nickname)
		config.UniqueEditLine = true
		c.Input.SetConfig(&config)
		fmt.Printf("--- %s (%s) ---\n", c.CurrentContact.Nickname, strings.ToLower(c.CurrentContact.Status.String()))
	} else {
		config := *c.Input.Config
		config.Prompt = "> "
		config.UniqueEditLine = false
		c.Input.SetConfig(&config)
	}
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

			if c.CurrentContact != nil && c.CurrentContact.Id == contact.Id {
				c.SetCurrentContact(contact)
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

			if c.CurrentContact != nil && c.CurrentContact.Id == contact.Id {
				c.SetCurrentContact(nil)
			}

		default:
			log.Printf("Ignoring unknown contact event: %v", event)
		}
	}
}

func (c *Client) monitorConversations() {
	stream, err := c.Backend.MonitorConversations(context.Background(), &ricochet.MonitorConversationsRequest{})
	if err != nil {
		log.Printf("Initializing conversations monitor failed: %v", err)
		// XXX handle
		return
	}

	log.Printf("Monitoring conversations")

	for {
		event, err := stream.Recv()
		if err != nil {
			log.Printf("Conversations monitor error: %v", err)
			// XXX handle
			break
		}

		// XXX Should also handle POPULATE
		if event.Type != ricochet.ConversationEvent_RECEIVE &&
			event.Type != ricochet.ConversationEvent_SEND {
			continue
		}

		message := event.Msg
		if message == nil || message.Recipient == nil || message.Sender == nil {
			log.Printf("Ignoring invalid conversation event: %v", event)
			continue
		}

		var remoteContact *ricochet.Contact
		var remoteEntity *ricochet.Entity

		if !message.Sender.IsSelf {
			remoteEntity = message.Sender
		} else {
			remoteEntity = message.Recipient
		}

		for _, contact := range c.Contacts {
			if remoteEntity.ContactId == contact.Id && remoteEntity.Address == contact.Address {
				remoteContact = contact
				break
			}
		}

		if remoteContact == nil {
			log.Printf("Ignoring conversation event with unknown contact: %v", event)
			continue
		}

		if remoteContact == c.CurrentContact {
			// XXX so unsafe
			if message.Sender.IsSelf {
				fmt.Fprintf(c.Input.Stdout(), "\r%s > %s\n", remoteContact.Nickname, message.Text)
			} else {
				fmt.Fprintf(c.Input.Stdout(), "\r%s < %s\n", remoteContact.Nickname, message.Text)
			}
		} else if !message.Sender.IsSelf {
			fmt.Fprintf(c.Input.Stdout(), "\r---- %s < %s\n", remoteContact.Nickname, message.Text)
		}
	}
}
