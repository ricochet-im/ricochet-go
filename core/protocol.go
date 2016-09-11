package core

import (
	"encoding/asn1"
	protocol "github.com/s-rah/go-ricochet"
	"log"
	"net"
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
func (p *Protocol) Connect(address string) (*protocol.OpenConnection, error) {
	oc, err := p.service.Connect(address)
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
	log.Printf("protocol: OnAuthenticationProof, result: %v", result)
	oc.SendAuthenticationResult(channelID, result, isKnownContact)
	oc.IsAuthed = result
	oc.CloseChannel(channelID)
}

func (handler *protocolHandler) OnAuthenticationResult(oc *protocol.OpenConnection, channelID int32, result bool, isKnownContact bool) {
	log.Printf("protocol: OnAuthenticationResult, result: %v, known: %v", result, isKnownContact)
	oc.IsAuthed = result
}

// Contact Management
func (handler *protocolHandler) IsKnownContact(hostname string) bool {
	return true
}

func (handler *protocolHandler) OnContactRequest(oc *protocol.OpenConnection, channelID int32, nick string, message string) {
}

func (handler *protocolHandler) OnContactRequestAck(oc *protocol.OpenConnection, channelID int32, status string) {
}

// Managing Channels
func (handler *protocolHandler) OnOpenChannelRequest(oc *protocol.OpenConnection, channelID int32, channelType string) {
	oc.AckOpenChannel(channelID, channelType)
}

func (handler *protocolHandler) OnOpenChannelRequestSuccess(oc *protocol.OpenConnection, channelID int32) {
}
func (handler *protocolHandler) OnChannelClosed(oc *protocol.OpenConnection, channelID int32) {
}

// Chat Messages
func (handler *protocolHandler) OnChatMessage(oc *protocol.OpenConnection, channelID int32, messageID int32, message string) {
}
func (handler *protocolHandler) OnChatMessageAck(oc *protocol.OpenConnection, channelID int32, messageID int32) {
}

// Handle Errors
func (handler *protocolHandler) OnFailedChannelOpen(oc *protocol.OpenConnection, channelID int32, errorType string) {
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
