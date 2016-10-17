package main

import (
	"fmt"
	"github.com/ricochet-im/ricochet-go/rpc"
	"golang.org/x/net/context"
	"log"
)

type Client struct {
	Backend ricochet.RicochetCoreClient
	Ui      *UI

	ServerStatus ricochet.ServerStatusReply
	Identity     ricochet.Identity

	NetworkStatus ricochet.NetworkStatus
	Contacts      *ContactList

	monitorsChannel chan interface{}
	blockChannel    chan struct{}
	unblockChannel  chan struct{}

	populatedChannel       chan struct{}
	populatedNetwork       bool
	populatedContacts      bool
	populatedConversations bool
}

// XXX need to handle backend connection loss/reconnection..
func (c *Client) Initialize() error {
	c.Contacts = NewContactList()
	c.monitorsChannel = make(chan interface{}, 10)
	c.blockChannel = make(chan struct{})
	c.unblockChannel = make(chan struct{})
	c.populatedChannel = make(chan struct{})

	// Query server status and version
	status, err := c.Backend.GetServerStatus(context.Background(), &ricochet.ServerStatusRequest{
		RpcVersion: 1,
	})
	if err != nil {
		return err
	}
	c.ServerStatus = *status
	if status.RpcVersion != 1 {
		return fmt.Errorf("unsupported backend RPC version %d", status.RpcVersion)
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

	// Spawn routine to handle all events
	go c.Run()

	// Block until all state is populated
	<-c.populatedChannel
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
		case <-c.blockChannel:
			<-c.unblockChannel
		}
	}
}

func (c *Client) Block() {
	c.blockChannel <- struct{}{}
}

func (c *Client) Unblock() {
	c.unblockChannel <- struct{}{}
}

func (c *Client) IsInitialized() bool {
	return c.populatedChannel == nil
}

func (c *Client) checkIfPopulated() {
	if c.populatedChannel != nil && c.populatedContacts &&
		c.populatedConversations && c.populatedNetwork {
		close(c.populatedChannel)
		c.populatedChannel = nil
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
	c.populatedNetwork = true
	c.checkIfPopulated()
}

func (c *Client) onContactEvent(event *ricochet.ContactEvent) {
	if !c.populatedContacts && event.Type != ricochet.ContactEvent_POPULATE {
		log.Printf("Ignoring unexpected contact event during populate: %v", event)
		return
	}

	data := event.GetContact()

	switch event.Type {
	case ricochet.ContactEvent_POPULATE:
		if c.populatedContacts {
			log.Printf("Ignoring unexpected contact populate event: %v", event)
		} else if event.Subject == nil {
			// Populate is terminated by a nil subject
			c.populatedContacts = true
			log.Printf("Loaded %d contacts", len(c.Contacts.Contacts))
			c.checkIfPopulated()
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

		if c.Ui.CurrentContact == contact {
			c.Ui.SetCurrentContact(nil)
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

	if event.Type == ricochet.ConversationEvent_POPULATE && message == nil {
		c.populatedConversations = true
		c.checkIfPopulated()
		return
	}

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

	if event.Type != ricochet.ConversationEvent_POPULATE {
		c.Ui.PrintMessage(remoteContact, message.Sender.IsSelf, message.Text)
	}

	// XXX Shouldn't mark until displayed
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

func (c *Client) NetworkControlStatus() ricochet.TorControlStatus {
	if c.NetworkStatus.Control != nil {
		return *c.NetworkStatus.Control
	} else {
		return ricochet.TorControlStatus{}
	}
}

func (c *Client) NetworkConnectionStatus() ricochet.TorConnectionStatus {
	if c.NetworkStatus.Connection != nil {
		return *c.NetworkStatus.Connection
	} else {
		return ricochet.TorConnectionStatus{}
	}
}
