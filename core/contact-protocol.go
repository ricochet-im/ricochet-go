package core

import (
	"github.com/s-rah/go-ricochet/channels"
	"github.com/s-rah/go-ricochet/connection"
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
	handler.Init()

	handler.RegisterChannelHandler("im.ricochet.chat", func() channels.Handler {
		chat := &channels.ChatChannel{
			Handler: contact.Conversation(),
		}
		return chat
	})

	return handler
}
