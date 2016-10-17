package core

import (
	"errors"
	protocol "github.com/s-rah/go-ricochet"
	"github.com/ricochet-im/ricochet-go/core/utils"
	"github.com/ricochet-im/ricochet-go/rpc"
	"log"
	"math/rand"
	"sync"
	"time"
)

// XXX should have limits on backlog size/duration

type Conversation struct {
	Contact *Contact

	mutex sync.Mutex

	localEntity       *ricochet.Entity
	remoteEntity      *ricochet.Entity
	messages          []*ricochet.Message
	lastSentMessageId uint32

	events *utils.Publisher
}

func NewConversation(contact *Contact, remoteEntity *ricochet.Entity, eventStream *utils.Publisher) *Conversation {
	return &Conversation{
		Contact:      contact,
		localEntity:  &ricochet.Entity{IsSelf: true},
		remoteEntity: remoteEntity,
		messages:     make([]*ricochet.Message, 0),
		events:       eventStream,
	}
}

func (c *Conversation) EventMonitor() utils.Subscribable {
	return c.events
}

func (c *Conversation) Messages() []*ricochet.Message {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	re := make([]*ricochet.Message, 0, len(c.messages))
	for _, message := range c.messages {
		re = append(re, message)
	}
	return re
}

func (c *Conversation) Receive(id uint64, timestamp int64, text string) {
	message := &ricochet.Message{
		Sender:     c.remoteEntity,
		Recipient:  c.localEntity,
		Timestamp:  timestamp,
		Identifier: id,
		Status:     ricochet.Message_UNREAD,
		Text:       text,
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.messages = append(c.messages, message)
	event := ricochet.ConversationEvent{
		Type: ricochet.ConversationEvent_RECEIVE,
		Msg:  message,
	}
	c.events.Publish(event)
}

func (c *Conversation) UpdateSentStatus(id uint64, success bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	for i := len(c.messages) - 1; i >= 0; i-- {
		message := c.messages[i]
		if message.Status != ricochet.Message_SENDING || message.Identifier != id {
			continue
		}

		if success {
			message.Status = ricochet.Message_DELIVERED
		} else {
			message.Status = ricochet.Message_ERROR
		}

		event := ricochet.ConversationEvent{
			Type: ricochet.ConversationEvent_UPDATE,
			Msg:  message,
		}
		c.events.Publish(event)
		return
	}

	log.Printf("Ignoring ack for unknown message id %d", id)
}

func (c *Conversation) Send(text string) (*ricochet.Message, error) {
	if len(text) == 0 {
		return nil, errors.New("Message text is empty")
	} else if len(text) > 2000 {
		return nil, errors.New("Message is too long")
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.lastSentMessageId == 0 {
		// Rand is seeded by Ricochet.Init
		c.lastSentMessageId = rand.Uint32()
	}
	if c.lastSentMessageId++; c.lastSentMessageId == 0 {
		c.lastSentMessageId++
	}

	message := &ricochet.Message{
		Sender:     c.localEntity,
		Recipient:  c.remoteEntity,
		Timestamp:  time.Now().Unix(),
		Identifier: uint64(c.lastSentMessageId),
		Status:     ricochet.Message_QUEUED,
		Text:       text,
	}

	// XXX threading mess, and probably deadlockable. Need better API for conn.
	conn := c.Contact.Connection()
	if conn != nil {
		sendMessageToConnection(conn, message)
		message.Status = ricochet.Message_SENDING
	}

	c.messages = append(c.messages, message)
	event := ricochet.ConversationEvent{
		Type: ricochet.ConversationEvent_SEND,
		Msg:  message,
	}
	c.events.Publish(event)

	return message, nil
}

// Send all messages in the QUEUED state to the contact, if
// a connection is available. Should be called after a new
// connection is established.
func (c *Conversation) SendQueuedMessages() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	conn := c.Contact.Connection()
	if conn == nil {
		return 0
	}

	sent := 0
	for _, message := range c.messages {
		if message.Status != ricochet.Message_QUEUED {
			continue
		}

		sendMessageToConnection(conn, message)
		message.Status = ricochet.Message_SENDING
		sent++

		event := ricochet.ConversationEvent{
			Type: ricochet.ConversationEvent_UPDATE,
			Msg:  message,
		}
		c.events.Publish(event)
	}

	return sent
}

func sendMessageToConnection(conn *protocol.OpenConnection, message *ricochet.Message) {
	// XXX hardcoded channel IDs, also channel IDs shouldn't be exposed
	channelId := int32(7)
	if !conn.Client {
		channelId++
	}
	// XXX no error handling
	if conn.GetChannelType(channelId) != "im.ricochet.chat" {
		conn.OpenChatChannel(channelId)
	}

	// XXX no message IDs
	conn.SendMessage(channelId, message.Text)
}

// XXX This is inefficient -- it'll usually only be marking the last message
// or few messages. Need a better way to know what's unread.
func (c *Conversation) MarkReadBeforeMessage(msgId uint64) int {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	marked := 0
	for _, message := range c.messages {
		if message.Status == ricochet.Message_UNREAD {
			message.Status = ricochet.Message_READ
			marked++

			event := ricochet.ConversationEvent{
				Type: ricochet.ConversationEvent_UPDATE,
				Msg:  message,
			}
			c.events.Publish(event)
		}

		if message.Identifier == msgId && message.Recipient.IsSelf {
			break
		}
	}

	return marked
}
