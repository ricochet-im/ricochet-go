package application

import (
	"crypto/rsa"
	"github.com/s-rah/go-ricochet"
	"github.com/s-rah/go-ricochet/channels"
	"github.com/s-rah/go-ricochet/connection"
	"log"
	"net"
	"time"
)

// RicochetApplication bundles many useful constructs that are
// likely standard in a ricochet application
type RicochetApplication struct {
	contactManager        ContactManagerInterface
	privateKey            *rsa.PrivateKey
	chatMessageHandler    func(*RicochetApplicationInstance, uint32, time.Time, string)
	chatMessageAckHandler func(*RicochetApplicationInstance, uint32)
	l       net.Listener
}

type RicochetApplicationInstance struct {
	connection.AutoConnectionHandler
	connection            *connection.Connection
	RemoteHostname        string
	ChatMessageHandler    func(*RicochetApplicationInstance, uint32, time.Time, string)
	ChatMessageAckHandler func(*RicochetApplicationInstance, uint32)
}

func (rai *RicochetApplicationInstance) ContactRequest(name string, message string) string {
	return "Accepted"
}

func (rai *RicochetApplicationInstance) ContactRequestRejected() {
}
func (rai *RicochetApplicationInstance) ContactRequestAccepted() {
}
func (rai *RicochetApplicationInstance) ContactRequestError() {
}

func (rai *RicochetApplicationInstance) SendChatMessage(message string) {
	rai.connection.Do(func() error {
		// Technically this errors afte the second time but we can ignore it.
		rai.connection.RequestOpenChannel("im.ricochet.chat", rai)

		channel := rai.connection.Channel("im.ricochet.chat", channels.Outbound)
		if channel != nil {
			chatchannel, ok := channel.Handler.(*channels.ChatChannel)
			if ok {
				chatchannel.SendMessage(message)
			}
		}
		return nil
	})
}

func (rai *RicochetApplicationInstance) ChatMessage(messageID uint32, when time.Time, message string) bool {
	go rai.ChatMessageHandler(rai, messageID, when, message)
	return true
}

func (rai *RicochetApplicationInstance) ChatMessageAck(messageID uint32) {
	rai.ChatMessageAckHandler(rai, messageID)
}

func (ra *RicochetApplication) Init(pk *rsa.PrivateKey, cm ContactManagerInterface) {
	ra.privateKey = pk
	ra.contactManager = cm
	ra.chatMessageHandler = func(*RicochetApplicationInstance, uint32, time.Time, string) {}
	ra.chatMessageAckHandler = func(*RicochetApplicationInstance, uint32) {}
}

func (ra *RicochetApplication) OnChatMessage(call func(*RicochetApplicationInstance, uint32, time.Time, string)) {
	ra.chatMessageHandler = call
}

func (ra *RicochetApplication) OnChatMessageAck(call func(*RicochetApplicationInstance, uint32)) {
	ra.chatMessageAckHandler = call
}

func (ra *RicochetApplication) handleConnection(conn net.Conn) {
	rc, err := goricochet.NegotiateVersionInbound(conn)
	if err != nil {
		log.Printf("There was an error")
		conn.Close()
		return
	}

	ich := connection.HandleInboundConnection(rc)

	err = ich.ProcessAuthAsServer(ra.privateKey, ra.contactManager.LookupContact)
	if err != nil {
		log.Printf("There was an error")
		conn.Close()
		return
	}

	rai := new(RicochetApplicationInstance)
	rai.Init(ra.privateKey, "")
	rai.RemoteHostname = rc.RemoteHostname
	rai.connection = rc
	rai.ChatMessageHandler = ra.chatMessageHandler
	rai.ChatMessageAckHandler = ra.chatMessageAckHandler

	rai.RegisterChannelHandler("im.ricochet.contact.request", func() channels.Handler {
		contact := new(channels.ContactRequestChannel)
		contact.Handler = rai
		return contact
	})
	rai.RegisterChannelHandler("im.ricochet.chat", func() channels.Handler {
		chat := new(channels.ChatChannel)
		chat.Handler = rai
		return chat
	})
	rc.Process(rai)
}

func (ra *RicochetApplication) Shutdown () {
        log.Printf("Closing")
        ra.l.Close()
        log.Printf("Closed")
}

func (ra *RicochetApplication) Run(l net.Listener) {
	if ra.privateKey == nil || ra.contactManager == nil {
		return
	}
	ra.l = l
        var err error
	for err == nil {
		conn, err := ra.l.Accept()
		if err == nil {
			go ra.handleConnection(conn)
		} else {
		        log.Printf("Closing")
		        return
		}
	}
}
