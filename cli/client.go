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

	NetworkStatus  ricochet.NetworkStatus
	Contacts       *ContactList
	CurrentContact *Contact

	monitorsChannel   chan interface{}
	populatedContacts bool
}

// XXX need to handle backend connection loss/reconnection..
func (c *Client) Initialize() error {
	c.Contacts = NewContactList()
	c.monitorsChannel = make(chan interface{}, 10)

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
	// Conversation monitor isn't started until contacts are populated

	// XXX block until populated/initialized?
	return nil
}

func (c *Client) Run() {
	for {
		select {
		case v := <-c.monitorsChannel:
			switch event := v.(type) {
			case *ricochet.NetworkStatus:
				c.onNetworkStatus(event)
			case *ricochet.ContactEvent:
				c.onContactEvent(event)
			case *ricochet.ConversationEvent:
				c.onConversationEvent(event)
			default:
				log.Panicf("Unknown event type on monitor channel: %v", event)
			}
		}
	}
}

func (c *Client) SetCurrentContact(contact *Contact) {
	c.CurrentContact = contact
	if c.CurrentContact != nil {
		config := *c.Input.Config
		config.Prompt = fmt.Sprintf("%s > ", c.CurrentContact.Data.Nickname)
		config.UniqueEditLine = true
		c.Input.SetConfig(&config)
		fmt.Printf("--- %s (%s) ---\n", c.CurrentContact.Data.Nickname, strings.ToLower(c.CurrentContact.Data.Status.String()))
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

		c.monitorsChannel <- status
	}
}

func (c *Client) monitorContacts() {
	stream, err := c.Backend.MonitorContacts(context.Background(), &ricochet.MonitorContactsRequest{})
	if err != nil {
		log.Printf("Initializing contact status monitor failed: %v", err)
		// XXX handle
		return
	}

	for {
		event, err := stream.Recv()
		if err != nil {
			log.Printf("Contact monitor error: %v", err)
			// XXX handle
			break
		}

		c.monitorsChannel <- event
	}
}

func (c *Client) monitorConversations() {
	stream, err := c.Backend.MonitorConversations(context.Background(), &ricochet.MonitorConversationsRequest{})
	if err != nil {
		log.Printf("Initializing conversations monitor failed: %v", err)
		// XXX handle
		return
	}

	for {
		event, err := stream.Recv()
		if err != nil {
			log.Printf("Conversations monitor error: %v", err)
			// XXX handle
			break
		}

		c.monitorsChannel <- event
	}
}

func (c *Client) onNetworkStatus(status *ricochet.NetworkStatus) {
	log.Printf("Network status changed: %v", status)
	c.NetworkStatus = *status
}

func (c *Client) onContactEvent(event *ricochet.ContactEvent) {
	if !c.populatedContacts && event.Type != ricochet.ContactEvent_POPULATE {
		log.Printf("Ignoring unexpected contact event during populate: %v", event)
		return
	}

	data := event.GetContact()

	switch event.Type {
	case ricochet.ContactEvent_POPULATE:
		// Populate is terminated by a nil subject
		if event.Subject == nil {
			c.populatedContacts = true
			log.Printf("Loaded %d contacts", len(c.Contacts.Contacts))
			go c.monitorConversations()
		} else if data != nil {
			c.Contacts.Populate(data)
		} else {
			log.Printf("Invalid contact populate event: %v", event)
		}

	case ricochet.ContactEvent_ADD:
		if data == nil {
			log.Printf("Ignoring contact add event with null data")
			return
		}

		c.Contacts.Added(data)

	case ricochet.ContactEvent_UPDATE:
		if data == nil {
			log.Printf("Ignoring contact update event with null data")
			return
		}

		contact := c.Contacts.ByIdAndAddress(data.Id, data.Address)
		if contact == nil {
			log.Printf("Ignoring contact update event for unknown contact: %v", data)
		} else {
			contact.Updated(data)
		}

	case ricochet.ContactEvent_DELETE:
		if data == nil {
			log.Printf("Ignoring contact delete event with null data")
			return
		}

		contact, _ := c.Contacts.Deleted(data)

		if c.CurrentContact == contact {
			c.SetCurrentContact(nil)
		}

	default:
		log.Printf("Ignoring unknown contact event: %v", event)
	}
}

func (c *Client) onConversationEvent(event *ricochet.ConversationEvent) {
	if event.Type != ricochet.ConversationEvent_RECEIVE &&
		event.Type != ricochet.ConversationEvent_SEND &&
		event.Type != ricochet.ConversationEvent_POPULATE {
		return
	}

	message := event.Msg
	if message == nil || message.Recipient == nil || message.Sender == nil {
		log.Printf("Ignoring invalid conversation event: %v", event)
		return
	}

	var remoteEntity *ricochet.Entity
	if !message.Sender.IsSelf {
		remoteEntity = message.Sender
	} else {
		remoteEntity = message.Recipient
	}

	remoteContact := c.Contacts.ByIdAndAddress(remoteEntity.ContactId, remoteEntity.Address)
	if remoteContact == nil {
		log.Printf("Ignoring conversation event with unknown contact: %v", event)
		return
	}

	if remoteContact == c.CurrentContact {
		// XXX so unsafe
		if message.Sender.IsSelf {
			fmt.Fprintf(c.Input.Stdout(), "\r%s > %s\n", remoteContact.Data.Nickname, message.Text)
		} else {
			fmt.Fprintf(c.Input.Stdout(), "\r%s < %s\n", remoteContact.Data.Nickname, message.Text)
		}
	} else if !message.Sender.IsSelf {
		fmt.Fprintf(c.Input.Stdout(), "\r---- %s < %s\n", remoteContact.Data.Nickname, message.Text)
	}

	if !message.Sender.IsSelf {
		backend := c.Backend
		message := message
		go func() {
			_, err := backend.MarkConversationRead(context.Background(), &ricochet.MarkConversationReadRequest{
				Entity:             message.Sender,
				LastRecvIdentifier: message.Identifier,
			})
			if err != nil {
				log.Printf("Mark conversation read failed: %v", err)
			}
		}()
	}
}
