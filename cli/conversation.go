package main

import (
	"errors"
	"fmt"
	"github.com/ricochet-im/ricochet-go/rpc"
	"golang.org/x/net/context"
	"log"
	"time"
)

const (
	maxMessageTextLength = 2000
	// Number of messages before the first unread message shown when
	// opening a conversation
	backlogContextNum = 3
	// Maximum number of messages to keep in the backlog. Unread messages
	// will never be discarded to keep this limit, and at least
	// backlogContextNum messages are kept before the first unread message.
	backlogSoftLimit = 100
	// Hard limit for the maximum numer of messages to keep in the backlog
	backlogHardLimit = 200
)

type Conversation struct {
	Client  *Client
	Contact *Contact

	messages []*ricochet.Message
}

// Send an outbound message to the contact and add that message into the
// conversation backlog. Blocking API call.
func (c *Conversation) SendMessage(text string) error {
	msg, err := c.Client.Backend.SendMessage(context.Background(), &ricochet.Message{
		Sender: &ricochet.Entity{IsSelf: true},
		Recipient: &ricochet.Entity{
			ContactId: c.Contact.Data.Id,
			Address:   c.Contact.Data.Address,
		},
		Text: text,
	})
	if err != nil {
		fmt.Printf("send message error: %v\n", err)
		return err
	}

	if err := c.validateMessage(msg); err != nil {
		log.Printf("Conversation sent message does not validate: %v", err)
	}
	c.messages = append(c.messages, msg)
	c.trimBacklog()
	c.printMessage(msg)

	return nil
}

// Add a message to the conversation. The message can be inbound or
// outbound. This is called for message events from the backend, and should
// not be used when sending messages. If 'populating' is true, this message
// is part of the initial sync of the history from the backend.
func (c *Conversation) AddMessage(msg *ricochet.Message, populating bool) {
	if err := c.validateMessage(msg); err != nil {
		log.Printf("Rejected conversation message: %v", err)
		return
	}

	c.messages = append(c.messages, msg)
	c.trimBacklog()
	if !populating {
		// XXX Need to do mark-as-read when displaying received messages in
		// an active conversation.
		// XXX Also, need to limit prints to the active conversation.
		// XXX Quite possibly, more of the active conversation logic belongs here.
		c.printMessage(msg)
	}
}

// XXX
func (c *Conversation) AddStatusMessage(text string, backlog bool) {
}

// Mark all unread messages in this conversation as read on the backend.
func (c *Conversation) MarkAsRead() error {
	// Get the identifier of the last received message
	var lastRecvMsg *ricochet.Message

findMessageId:
	for i := len(c.messages) - 1; i >= 0; i-- {
		switch c.messages[i].Status {
		case ricochet.Message_UNREAD:
			lastRecvMsg = c.messages[i]
			break findMessageId
		case ricochet.Message_READ:
			break findMessageId
		}
	}

	if lastRecvMsg != nil {
		return c.MarkAsReadBefore(lastRecvMsg)
	}
	return nil
}

func (c *Conversation) MarkAsReadBefore(message *ricochet.Message) error {
	if err := c.validateMessage(message); err != nil {
		return err
	} else if message.Sender.IsSelf {
		return errors.New("Outbound messages cannot be marked as read")
	}

	// XXX This probably means it's impossible to mark messages as read
	// if the sender uses 0 identifiers. We really should not use actual
	// protocol identifiers in RPC API.
	_, err := c.Client.Backend.MarkConversationRead(context.Background(),
		&ricochet.MarkConversationReadRequest{
			Entity:             message.Sender,
			LastRecvIdentifier: message.Identifier,
		})
	if err != nil {
		log.Printf("Mark conversation read failed: %v", err)
	}
	return err
}

func (c *Conversation) PrintContext() {
	// Print starting from backlogContextNum messages before the first unread
	start := len(c.messages) - backlogContextNum
	for i, message := range c.messages {
		if message.Status == ricochet.Message_UNREAD {
			start = i - backlogContextNum
			break
		}
	}

	if start < 0 {
		start = 0
	}

	for i := start; i < len(c.messages); i++ {
		c.printMessage(c.messages[i])
	}
}

func (c *Conversation) trimBacklog() {
	if len(c.messages) > backlogHardLimit {
		c.messages = c.messages[len(c.messages)-backlogHardLimit:]
	}
	if len(c.messages) <= backlogSoftLimit {
		return
	}

	// Find the index of the oldest unread message
	var keepIndex int
	for i, message := range c.messages {
		if message.Status == ricochet.Message_UNREAD {
			// Keep backlogContextNum messages before the first unread one
			keepIndex = i - backlogContextNum
			if keepIndex < 0 {
				keepIndex = 0
			}
			break
		} else if len(c.messages)-i <= backlogSoftLimit {
			// Remove all messages before this one to reduce to the limit
			keepIndex = i
			break
		}
	}

	c.messages = c.messages[keepIndex:]
}

// Validate that a message object is well-formed, sane, and belongs
// to this conversation.
func (c *Conversation) validateMessage(msg *ricochet.Message) error {
	if msg.Sender == nil || msg.Recipient == nil {
		return fmt.Errorf("Message entities are incomplete: %v %v", msg.Sender, msg.Recipient)
	}

	var localEntity *ricochet.Entity
	var remoteEntity *ricochet.Entity
	if msg.Sender.IsSelf {
		localEntity = msg.Sender
		remoteEntity = msg.Recipient
	} else {
		localEntity = msg.Recipient
		remoteEntity = msg.Sender
	}

	if !localEntity.IsSelf || localEntity.ContactId != 0 ||
		(len(localEntity.Address) > 0 && localEntity.Address != c.Client.Identity.Address) {
		return fmt.Errorf("Invalid self entity on message: %v", localEntity)
	}

	if remoteEntity.IsSelf || remoteEntity.ContactId != c.Contact.Data.Id ||
		remoteEntity.Address != c.Contact.Data.Address {
		return fmt.Errorf("Invalid remote entity on message: %v", remoteEntity)
	}

	// XXX timestamp
	// XXX identifier

	if msg.Status == ricochet.Message_NULL {
		return fmt.Errorf("Message has null status: %v", msg)
	}

	// XXX more sanity checks on message text?
	if len(msg.Text) == 0 || len(msg.Text) > maxMessageTextLength {
		return errors.New("Message text is unacceptable")
	}

	return nil
}

func (c *Conversation) printMessage(msg *ricochet.Message) {
	// XXX actual timestamp
	ts := "\x1b[90m" + time.Now().Format("15:04") + "\x1b[39m"

	var direction string
	if msg.Sender.IsSelf {
		direction = "\x1b[34m<<\x1b[39m"
	} else {
		direction = "\x1b[31m>>\x1b[39m"
	}

	// XXX shell escaping
	fmt.Fprintf(Ui.Input.Stdout(), "\r%s | %s %s %s\n",
		ts,
		c.Contact.Data.Nickname,
		direction,
		msg.Text)

	/*
		fmt.Fprintf(ui.Input.Stdout(), "\r\x1b[31m( \x1b[39mNew message from \x1b[31m%s\x1b[39m -- \x1b[1m/%d\x1b[0m to view \x1b[31m)\x1b[39m\n", contact.Data.Nickname, contact.Data.Id)
	*/
}
