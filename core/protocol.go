package core

import (
	"encoding/asn1"
	protocol "github.com/s-rah/go-ricochet"
	"log"
	"net"
	"time"
)

type Protocol struct {
	core *Ricochet

	service *protocol.Ricochet
	handler *protocolHandler
}

// Implements protocol.RicochetService
type protocolHandler struct {
	p *Protocol
}

func CreateProtocol(core *Ricochet) *Protocol {
	p := &Protocol{
		core:    core,
		service: new(protocol.Ricochet),
	}
	p.handler = &protocolHandler{p: p}
	p.service.Init()
	return p
}

func (p *Protocol) ServeListener(listener net.Listener) {
	p.service.ServeListener(p.handler, listener)
}

// Strangely, ServeListener starts a background routine that watches a channel
// on p.service for new connections and dispatches their events to the handler
// for the listener. API needs a little work here.
func (p *Protocol) ConnectOpen(conn net.Conn, host string) (*protocol.OpenConnection, error) {
	oc, err := p.service.ConnectOpen(conn, host)
	if err != nil {
		return nil, err
	}
	oc.MyHostname = p.core.Identity.Address()[9:]
	return oc, nil
}

func (handler *protocolHandler) OnReady() {
	log.Printf("protocol: OnReady")
}

func (handler *protocolHandler) OnConnect(oc *protocol.OpenConnection) {
	log.Printf("protocol: OnConnect: %v", oc)
	if oc.Client {
		log.Printf("Connected to %s", oc.OtherHostname)
		oc.IsAuthed = true // Outbound connections are authenticated
		oc.Authenticate(1)
	} else {
		// Strip ricochet:
		oc.MyHostname = handler.p.core.Identity.Address()[9:]
	}
}

func (handler *protocolHandler) OnDisconnect(oc *protocol.OpenConnection) {
	log.Printf("protocol: OnDisconnect: %v", oc)
	if oc.OtherHostname != "" {
		contact := handler.p.core.Identity.ContactList().ContactByAddress("ricochet:" + oc.OtherHostname)
		if contact != nil {
			contact.OnConnectionClosed(oc)
		}
	}
}

// Authentication Management
func (handler *protocolHandler) OnAuthenticationRequest(oc *protocol.OpenConnection, channelID int32, clientCookie [16]byte) {
	log.Printf("protocol: OnAuthenticationRequest")
	oc.ConfirmAuthChannel(channelID, clientCookie)
}

func (handler *protocolHandler) OnAuthenticationChallenge(oc *protocol.OpenConnection, channelID int32, serverCookie [16]byte) {
	log.Printf("protocol: OnAuthenticationChallenge")
	privateKey := handler.p.core.Identity.PrivateKey()
	publicKeyBytes, _ := asn1.Marshal(privateKey.PublicKey)
	oc.SendProof(1, serverCookie, publicKeyBytes, &privateKey)
}

func (handler *protocolHandler) OnAuthenticationProof(oc *protocol.OpenConnection, channelID int32, publicKey []byte, signature []byte, isKnownContact bool) {
	result := oc.ValidateProof(channelID, publicKey, signature)

	var contact *Contact
	if result {
		if len(oc.OtherHostname) != 16 {
			log.Printf("protocol: Invalid format for hostname '%s' in authentication proof", oc.OtherHostname)
			result = false
		} else {
			contact = handler.p.core.Identity.ContactList().ContactByAddress("ricochet:" + oc.OtherHostname)
		}
	}
	isKnownContact = (contact != nil)

	oc.SendAuthenticationResult(channelID, result, isKnownContact)
	oc.IsAuthed = result
	oc.CloseChannel(channelID)

	log.Printf("protocol: OnAuthenticationProof, result: %v, contact: %v", result, contact)
	if result && contact != nil {
		contact.OnConnectionAuthenticated(oc)
	}
}

func (handler *protocolHandler) OnAuthenticationResult(oc *protocol.OpenConnection, channelID int32, result bool, isKnownContact bool) {
	oc.IsAuthed = result
	oc.CloseChannel(channelID)

	if !result {
		log.Printf("protocol: Outbound connection authentication to %s failed", oc.OtherHostname)
		oc.Close()
		return
	}

	// XXX Contact request, removed cases
	if !isKnownContact {
		log.Printf("protocol: Outbound connection authentication to %s succeeded, but we are not a known contact", oc.OtherHostname)
		oc.Close()
		return
	}

	contact := handler.p.core.Identity.ContactList().ContactByAddress("ricochet:" + oc.OtherHostname)
	if contact == nil {
		log.Printf("protocol: Outbound connection authenticated to %s succeeded, but no matching contact found", oc.OtherHostname)
		oc.Close()
		return
	}

	log.Printf("protocol: Outbound connection to %s authenticated", oc.OtherHostname)
	contact.OnConnectionAuthenticated(oc)
}

// Contact Management
func (handler *protocolHandler) IsKnownContact(hostname string) bool {
	contact := handler.p.core.Identity.ContactList().ContactByAddress("ricochet:" + hostname)
	return contact != nil
}

func (handler *protocolHandler) OnContactRequest(oc *protocol.OpenConnection, channelID int32, nick string, message string) {
}

func (handler *protocolHandler) OnContactRequestAck(oc *protocol.OpenConnection, channelID int32, status string) {
}

// Managing Channels
func (handler *protocolHandler) OnOpenChannelRequest(oc *protocol.OpenConnection, channelID int32, channelType string) {
	log.Printf("open channel request: %v %v", channelID, channelType)
	oc.AckOpenChannel(channelID, channelType)
}

func (handler *protocolHandler) OnOpenChannelRequestSuccess(oc *protocol.OpenConnection, channelID int32) {
	log.Printf("open channel request success: %v %v", channelID)
}
func (handler *protocolHandler) OnChannelClosed(oc *protocol.OpenConnection, channelID int32) {
	log.Printf("channel closed: %v", channelID)
}

// Chat Messages
// XXX messageID should be (at least) uint32
func (handler *protocolHandler) OnChatMessage(oc *protocol.OpenConnection, channelID int32, messageID int32, message string) {
	// XXX no time delta?
	// XXX sanity checks, message contents, etc
	log.Printf("chat message: %d %d %s", channelID, messageID, message)

	// XXX ugllly
	contact := handler.p.core.Identity.ContactList().ContactByAddress("ricochet:" + oc.OtherHostname)
	if contact != nil {
		conversation := contact.Conversation()
		conversation.Receive(uint64(messageID), time.Now().Unix(), message)
	}

	oc.AckChatMessage(channelID, messageID)
}
func (handler *protocolHandler) OnChatMessageAck(oc *protocol.OpenConnection, channelID int32, messageID int32) {
	// XXX no success
	log.Printf("chat ack: %d %d", channelID, messageID)

	// XXX Also ugly
	contact := handler.p.core.Identity.ContactList().ContactByAddress("ricochet:" + oc.OtherHostname)
	if contact != nil {
		conversation := contact.Conversation()
		conversation.UpdateSentStatus(uint64(messageID), true)
	}
}

// Handle Errors
func (handler *protocolHandler) OnFailedChannelOpen(oc *protocol.OpenConnection, channelID int32, errorType string) {
	log.Printf("failed channel open: %d %s", channelID, errorType)
	oc.UnsetChannel(channelID)
}
func (handler *protocolHandler) OnGenericError(oc *protocol.OpenConnection, channelID int32) {
	oc.RejectOpenChannel(channelID, "GenericError")
}
func (handler *protocolHandler) OnUnknownTypeError(oc *protocol.OpenConnection, channelID int32) {
	oc.RejectOpenChannel(channelID, "UnknownTypeError")
}
func (handler *protocolHandler) OnUnauthorizedError(oc *protocol.OpenConnection, channelID int32) {
	oc.RejectOpenChannel(channelID, "UnauthorizedError")
}
func (handler *protocolHandler) OnBadUsageError(oc *protocol.OpenConnection, channelID int32) {
	oc.RejectOpenChannel(channelID, "BadUsageError")
}
func (handler *protocolHandler) OnFailedError(oc *protocol.OpenConnection, channelID int32) {
	oc.RejectOpenChannel(channelID, "FailedError")
}
