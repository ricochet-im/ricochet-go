package core

import (
	"errors"
	"github.com/ricochet-im/ricochet-go/core/utils"
	"github.com/ricochet-im/ricochet-go/rpc"
	"github.com/s-rah/go-ricochet/channels"
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

	// XXX The Qt implementation would discard duplicate messages by checking
	// the most recent 5 messages for any received message that matches this one
	// in both id and text. Should do that here.

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

	if online, err := c.sendMessageToConnection(message); err != nil {
		if online {
			message.Status = ricochet.Message_ERROR
		} else {
			message.Status = ricochet.Message_QUEUED
		}
	} else {
		// XXX go-ricochet doesn't support message IDs & ack properly yet, so skip SENDING
		//message.Status = ricochet.Message_SENDING
		message.Status = ricochet.Message_DELIVERED
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

		if online, err := c.sendMessageToConnection(message); err != nil {
			if online {
				message.Status = ricochet.Message_ERROR
			} else {
				// Offline again?
				break
			}
		} else {
			// XXX go-ricochet doesn't support message IDs & ack properly yet, so skip SENDING
			//message.Status = ricochet.Message_SENDING
			message.Status = ricochet.Message_DELIVERED
			sent++
		}

		event := ricochet.ConversationEvent{
			Type: ricochet.ConversationEvent_UPDATE,
			Msg:  message,
		}
		c.events.Publish(event)
	}

	return sent
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

// Implement ChatChannelHandler (im.ricochet.chat)
func (c *Conversation) ChatMessage(messageID uint32, when time.Time, message string) bool {
	// XXX sanity checks, message contents, etc
	log.Printf("chat message: %d %d %v %s", messageID, when, message)

	c.Receive(uint64(messageID), when.Unix(), message)
	return true
}

func (c *Conversation) ChatMessageAck(messageID uint32) {
	// XXX no success field
	log.Printf("chat ack: %d", messageID)

	c.UpdateSentStatus(uint64(messageID), true)
}

func (c *Conversation) sendMessageToConnection(message *ricochet.Message) (connected bool, err error) {
	conn := c.Contact.Connection()
	if conn == nil {
		err = errors.New("not connected")
		return
	}
	connected = true

	err = conn.Do(func() error {
		channel := conn.Channel("im.ricochet.chat", channels.Outbound)
		if channel == nil {
			if ch, err := conn.RequestOpenChannel("im.ricochet.chat", &channels.ChatChannel{Handler: c}); err != nil {
				return err
			} else {
				channel = ch
			}
		}
		chat, ok := channel.Handler.(*channels.ChatChannel)
		if !ok {
			channel.CloseChannel()
			return errors.New("invalid chat channel")
		}

		// XXX message id and all of that
		chat.SendMessage(message.Text)
		return nil
	})

	if err != nil {
		log.Printf("chat send failed: %s", err)
	}
	return
}
