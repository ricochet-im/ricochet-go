package core

import (
	"github.com/s-rah/go-ricochet/channels"
	"github.com/s-rah/go-ricochet/connection"
	"log"
	"time"
)

type ContactProtocolHandler struct {
	connection.AutoConnectionHandler
	conn    *connection.Connection
	contact *Contact
}

func NewContactProtocolHandler(contact *Contact, conn *connection.Connection) *ContactProtocolHandler {
	handler := &ContactProtocolHandler{
		conn:    conn,
		contact: contact,
	}
	handler.Init(nil, conn.RemoteHostname)

	handler.RegisterChannelHandler("im.ricochet.chat", func() channels.Handler {
		chat := new(channels.ChatChannel)
		chat.Handler = handler
		return chat
	})

	// XXX Somebody needs to be calling Process, nobody is yet, need that rework in contact
	return handler
}

// Implement ChatChannelHandler for im.ricochet.chat
func (handler *ContactProtocolHandler) ChatMessage(messageID uint32, when time.Time, message string) bool {
	// XXX sanity checks, message contents, etc
	log.Printf("chat message: %d %d %v %s", messageID, when, message)

	conversation := handler.contact.Conversation()
	conversation.Receive(uint64(messageID), when.Unix(), message)
	return true
}

func (handler *ContactProtocolHandler) ChatMessageAck(messageID uint32) {
	// XXX no success field
	log.Printf("chat ack: %d", messageID)

	conversation := handler.contact.Conversation()
	conversation.UpdateSentStatus(uint64(messageID), true)
}
