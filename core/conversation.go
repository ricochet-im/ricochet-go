package core

import (
	"github.com/special/notricochet/core/utils"
	"github.com/special/notricochet/rpc"
	"log"
	"sync"
	"time"
)

// XXX threading model.. this one isn't great

// XXX Should probably be under core or identity
var conversationStream *utils.Publisher = utils.CreatePublisher()

type Conversation struct {
	Core    *Ricochet
	Contact *Contact

	mutex sync.Mutex

	localEntity  *ricochet.Entity
	remoteEntity *ricochet.Entity
	messages     []*ricochet.Message
}

func NewConversation(core *Ricochet, contact *Contact, remoteEntity *ricochet.Entity) *Conversation {
	return &Conversation{
		Core:         core,
		Contact:      contact,
		localEntity:  &ricochet.Entity{IsSelf: true},
		remoteEntity: remoteEntity,
		messages:     make([]*ricochet.Message, 0),
	}
}

func ConversationEventMonitor() utils.Subscribable {
	return conversationStream
}

func (c *Conversation) Receive(id uint64, timestamp int64, text string) {
	message := &ricochet.Message{
		Sender:     c.remoteEntity,
		Recipient:  c.localEntity,
		Timestamp:  timestamp,
		Identifier: id,
		Status:     ricochet.Message_RECEIVED,
		Text:       text,
	}
	// XXX container
	// XXX limit backlog/etc
	c.mutex.Lock()
	c.messages = append(c.messages, message)
	log.Printf("Conversation received message: %v", message)
	c.mutex.Unlock()

	// XXX Technically these aren't guaranteed to be in order (because
	// the lock has been released) or to all arrive (because of publisher's
	// dropping behavior)...
	event := ricochet.ConversationEvent{
		Type: ricochet.ConversationEvent_RECEIVE,
		Msg:  message,
	}
	conversationStream.Publish(event)
}

func (c *Conversation) UpdateSentStatus(id uint64, success bool) {
	c.mutex.Lock()
	for _, message := range c.messages {
		if message.Status != ricochet.Message_SENDING || message.Identifier != id {
			continue
		}

		if success {
			message.Status = ricochet.Message_DELIVERED
		} else {
			message.Status = ricochet.Message_ERROR
		}

		c.mutex.Unlock()
		event := ricochet.ConversationEvent{
			Type: ricochet.ConversationEvent_UPDATE,
			Msg:  message,
		}
		conversationStream.Publish(event)
		return
	}
	c.mutex.Unlock()
}

func (c *Conversation) Send(text string) {
	// XXX protocol
	// XXX check that text is ok, get identifier, etc
	// XXX decide whether sending or queued based on state
	message := &ricochet.Message{
		Sender:     c.localEntity,
		Recipient:  c.remoteEntity,
		Timestamp:  time.Now().Unix(),
		Identifier: 0,                        // XXX
		Status:     ricochet.Message_SENDING, // XXX
		Text:       text,
	}
	c.mutex.Lock()
	c.messages = append(c.messages, message)
	log.Printf("Conversation sent message: %v", message)
	c.mutex.Unlock()

	event := ricochet.ConversationEvent{
		Type: ricochet.ConversationEvent_SEND,
		Msg:  message,
	}
	conversationStream.Publish(event)
}
