package goricochet

import (
	"crypto"
	"crypto/rsa"
	"encoding/asn1"
	"github.com/golang/protobuf/proto"
	"github.com/s-rah/go-ricochet/auth"
	"github.com/s-rah/go-ricochet/chat"
	"github.com/s-rah/go-ricochet/contact"
	"github.com/s-rah/go-ricochet/control"
	"github.com/s-rah/go-ricochet/utils"
	"log"
	"net"
)

// OpenConnection encapsulates the state required to maintain a connection to
// a ricochet service.
// Notably OpenConnection does not enforce limits on the channelIDs, channel Assignments
// or the direction of messages. These are considered to be service enforced rules.
// (and services are considered to be the best to define them).
type OpenConnection struct {
	conn        net.Conn
	authHandler map[int32]*AuthenticationHandler
	channels    map[int32]string
	rni         utils.RicochetNetworkInterface

	Client        bool
	IsAuthed      bool
	MyHostname    string
	OtherHostname string
	Closed        bool
}

// Init initializes a OpenConnection object to a default state.
func (oc *OpenConnection) Init(outbound bool, conn net.Conn) {
	oc.conn = conn
	oc.authHandler = make(map[int32]*AuthenticationHandler)
	oc.channels = make(map[int32]string)
	oc.rni = new(utils.RicochetNetwork)

	oc.Client = outbound
	oc.IsAuthed = false
	oc.MyHostname = ""
	oc.OtherHostname = ""
}

// UnsetChannel removes a type association from the channel.
func (oc *OpenConnection) UnsetChannel(channel int32) {
	oc.channels[channel] = "none"
}

// GetChannelType returns the type of the channel on this connection
func (oc *OpenConnection) GetChannelType(channel int32) string {
	if val, ok := oc.channels[channel]; ok {
		return val
	}
	return "none"
}

func (oc *OpenConnection) setChannel(channel int32, channelType string) {
	oc.channels[channel] = channelType
}

// HasChannel returns true if the connection has a channel of an associated type, false otherwise
func (oc *OpenConnection) HasChannel(channelType string) bool {
	for _, val := range oc.channels {
		if val == channelType {
			return true
		}
	}
	return false
}

// CloseChannel closes a given channel
// Prerequisites:
//              * Must have previously connected to a service
func (oc *OpenConnection) CloseChannel(channel int32) {
	oc.UnsetChannel(channel)
	oc.rni.SendRicochetPacket(oc.conn, channel, []byte{})
}

// Close closes the entire connection
func (oc *OpenConnection) Close() {
	oc.conn.Close()
	oc.Closed = true
}

// Authenticate opens an Authentication Channel and send a client cookie
// Prerequisites:
//              * Must have previously connected to a service
func (oc *OpenConnection) Authenticate(channel int32) {
	defer utils.RecoverFromError()

	oc.authHandler[channel] = new(AuthenticationHandler)
	messageBuilder := new(MessageBuilder)
	data, err := messageBuilder.OpenAuthenticationChannel(channel, oc.authHandler[channel].GenClientCookie())
	utils.CheckError(err)

	oc.setChannel(channel, "im.ricochet.auth.hidden-service")
	oc.rni.SendRicochetPacket(oc.conn, 0, data)
}

// ConfirmAuthChannel responds to a new authentication request.
// Prerequisites:
//              * Must have previously connected to a service
func (oc *OpenConnection) ConfirmAuthChannel(channel int32, clientCookie [16]byte) {
	defer utils.RecoverFromError()

	oc.authHandler[channel] = new(AuthenticationHandler)
	oc.authHandler[channel].AddClientCookie(clientCookie[:])
	messageBuilder := new(MessageBuilder)
	data, err := messageBuilder.ConfirmAuthChannel(channel, oc.authHandler[channel].GenServerCookie())
	utils.CheckError(err)

	oc.setChannel(channel, "im.ricochet.auth.hidden-service")
	oc.rni.SendRicochetPacket(oc.conn, 0, data)
}

// SendProof sends an authentication proof in response to a challenge.
// Prerequisites:
//              * Must have previously connected to a service
//              * channel must be of type auth
func (oc *OpenConnection) SendProof(channel int32, serverCookie [16]byte, publicKeyBytes []byte, privateKey *rsa.PrivateKey) {

	if oc.authHandler[channel] == nil {
		return // NoOp
	}

	oc.authHandler[channel].AddServerCookie(serverCookie[:])

	challenge := oc.authHandler[channel].GenChallenge(oc.MyHostname, oc.OtherHostname)
	signature, _ := rsa.SignPKCS1v15(nil, privateKey, crypto.SHA256, challenge)

	defer utils.RecoverFromError()
	messageBuilder := new(MessageBuilder)
	data, err := messageBuilder.Proof(publicKeyBytes, signature)
	utils.CheckError(err)

	oc.rni.SendRicochetPacket(oc.conn, channel, data)
}

// ValidateProof determines if the given public key and signature align with the
// already established challenge vector for this communication
// Prerequisites:
//              * Must have previously connected to a service
//              * Client and Server must have already sent their respective cookies (Authenticate and ConfirmAuthChannel)
func (oc *OpenConnection) ValidateProof(channel int32, publicKeyBytes []byte, signature []byte) bool {

	if oc.authHandler[channel] == nil {
		return false
	}

	provisionalHostname := utils.GetTorHostname(publicKeyBytes)
	publicKey := new(rsa.PublicKey)
	_, err := asn1.Unmarshal(publicKeyBytes, publicKey)
	if err != nil {
		return false
	}
	challenge := oc.authHandler[channel].GenChallenge(provisionalHostname, oc.MyHostname)
	err = rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, challenge[:], signature)
	if err == nil {
		oc.OtherHostname = provisionalHostname
		return true
	}
	return false

}

// SendAuthenticationResult responds to an existed authentication Proof
// Prerequisites:
//              * Must have previously connected to a service
//              * channel must be of type auth
func (oc *OpenConnection) SendAuthenticationResult(channel int32, accepted bool, isKnownContact bool) {
	defer utils.RecoverFromError()
	messageBuilder := new(MessageBuilder)
	data, err := messageBuilder.AuthResult(accepted, isKnownContact)
	utils.CheckError(err)
	oc.rni.SendRicochetPacket(oc.conn, channel, data)
}

// OpenChatChannel opens a new chat channel with the given id
// Prerequisites:
//              * Must have previously connected to a service
//              * If acting as the client, id must be odd, else even
func (oc *OpenConnection) OpenChatChannel(channel int32) {
	defer utils.RecoverFromError()
	messageBuilder := new(MessageBuilder)
	data, err := messageBuilder.OpenChannel(channel, "im.ricochet.chat")
	utils.CheckError(err)

	oc.setChannel(channel, "im.ricochet.chat")
	oc.rni.SendRicochetPacket(oc.conn, 0, data)
}

// OpenChannel opens a new chat channel with the given id
// Prerequisites:
//              * Must have previously connected to a service
//              * If acting as the client, id must be odd, else even
func (oc *OpenConnection) OpenChannel(channel int32, channelType string) {
	defer utils.RecoverFromError()
	messageBuilder := new(MessageBuilder)
	data, err := messageBuilder.OpenChannel(channel, channelType)
	utils.CheckError(err)

	oc.setChannel(channel, channelType)
	oc.rni.SendRicochetPacket(oc.conn, 0, data)
}

// AckOpenChannel acknowledges a previously received open channel message
// Prerequisites:
//             * Must have previously connected and authenticated to a service
func (oc *OpenConnection) AckOpenChannel(channel int32, channeltype string) {
	defer utils.RecoverFromError()
	messageBuilder := new(MessageBuilder)

	data, err := messageBuilder.AckOpenChannel(channel)
	utils.CheckError(err)

	oc.setChannel(channel, channeltype)
	oc.rni.SendRicochetPacket(oc.conn, 0, data)
}

// RejectOpenChannel acknowledges a rejects a previously received open channel message
// Prerequisites:
//             * Must have previously connected
func (oc *OpenConnection) RejectOpenChannel(channel int32, errortype string) {
	defer utils.RecoverFromError()
	messageBuilder := new(MessageBuilder)
	data, err := messageBuilder.RejectOpenChannel(channel, errortype)
	utils.CheckError(err)

	oc.rni.SendRicochetPacket(oc.conn, 0, data)
}

// SendContactRequest initiates a contact request to the server.
// Prerequisites:
//             * Must have previously connected and authenticated to a service
func (oc *OpenConnection) SendContactRequest(channel int32, nick string, message string) {
	defer utils.RecoverFromError()

	messageBuilder := new(MessageBuilder)
	data, err := messageBuilder.OpenContactRequestChannel(channel, nick, message)
	utils.CheckError(err)

	oc.setChannel(channel, "im.ricochet.contact.request")
	oc.rni.SendRicochetPacket(oc.conn, 0, data)
}

// AckContactRequestOnResponse responds a contact request from a client
// Prerequisites:
//             * Must have previously connected and authenticated to a service
//             * Must have previously received a Contact Request
func (oc *OpenConnection) AckContactRequestOnResponse(channel int32, status string) {
	defer utils.RecoverFromError()

	messageBuilder := new(MessageBuilder)
	data, err := messageBuilder.ReplyToContactRequestOnResponse(channel, status)
	utils.CheckError(err)

	oc.setChannel(channel, "im.ricochet.contact.request")
	oc.rni.SendRicochetPacket(oc.conn, 0, data)
}

// AckContactRequest responds to contact request from a client
// Prerequisites:
//             * Must have previously connected and authenticated to a service
//             * Must have previously received a Contact Request
func (oc *OpenConnection) AckContactRequest(channel int32, status string) {
	defer utils.RecoverFromError()

	messageBuilder := new(MessageBuilder)
	data, err := messageBuilder.ReplyToContactRequest(channel, status)
	utils.CheckError(err)

	oc.setChannel(channel, "im.ricochet.contact.request")
	oc.rni.SendRicochetPacket(oc.conn, channel, data)
}

// AckChatMessage acknowledges a previously received chat message.
// Prerequisites:
//             * Must have previously connected and authenticated to a service
//             * Must have established a known contact status with the other service
//             * Must have received a Chat message on an open im.ricochet.chat channel with the messageID
func (oc *OpenConnection) AckChatMessage(channel int32, messageID int32) {
	defer utils.RecoverFromError()

	messageBuilder := new(MessageBuilder)
	data, err := messageBuilder.AckChatMessage(messageID)
	utils.CheckError(err)

	oc.rni.SendRicochetPacket(oc.conn, channel, data)
}

// SendMessage sends a Chat Message (message) to a give Channel (channel).
// Prerequisites:
//             * Must have previously connected and authenticated to a service
//             * Must have established a known contact status with the other service
//             * Must have previously opened channel with OpenChanel of type im.ricochet.chat
func (oc *OpenConnection) SendMessage(channel int32, message string) {
	defer utils.RecoverFromError()
	messageBuilder := new(MessageBuilder)
	data, err := messageBuilder.ChatMessage(message, 0)
	utils.CheckError(err)
	oc.rni.SendRicochetPacket(oc.conn, channel, data)
}

// Process waits for new messages to arrive from the connection and uses the given
// ConnectionHandler to process them.
func (oc *OpenConnection) Process(handler ConnectionHandler) {
	handler.OnReady(oc)
	defer oc.Close()
	defer handler.OnDisconnect()

	for {
		if oc.Closed {
			return
		}

		packet, err := oc.rni.RecvRicochetPacket(oc.conn)
		if err != nil {
			oc.Close()
			return
		}

		if len(packet.Data) == 0 {
			handler.OnChannelClosed(packet.Channel)
			continue
		}

		if packet.Channel == 0 {

			res := new(Protocol_Data_Control.Packet)
			err := proto.Unmarshal(packet.Data[:], res)

			if err != nil {
				handler.OnGenericError(packet.Channel)
				continue
			}

			if res.GetOpenChannel() != nil {
				opm := res.GetOpenChannel()

				if oc.GetChannelType(opm.GetChannelIdentifier()) != "none" {
					// Channel is already in use.
					handler.OnBadUsageError(opm.GetChannelIdentifier())
					continue
				}

				// If I am a Client, the server can only open even numbered channels
				if oc.Client && opm.GetChannelIdentifier()%2 != 0 {
					handler.OnBadUsageError(opm.GetChannelIdentifier())
					continue
				}

				// If I am a Server, the client can only open odd numbered channels
				if !oc.Client && opm.GetChannelIdentifier()%2 != 1 {
					handler.OnBadUsageError(opm.GetChannelIdentifier())
					continue
				}

				switch opm.GetChannelType() {
				case "im.ricochet.auth.hidden-service":
					if oc.Client {
						// Servers are authed by default and can't auth with hidden-service
						handler.OnBadUsageError(opm.GetChannelIdentifier())
					} else if oc.IsAuthed {
						// Can't auth if already authed
						handler.OnBadUsageError(opm.GetChannelIdentifier())
					} else if oc.HasChannel("im.ricochet.auth.hidden-service") {
						// Can't open more than 1 auth channel
						handler.OnBadUsageError(opm.GetChannelIdentifier())
					} else {
						clientCookie, err := proto.GetExtension(opm, Protocol_Data_AuthHiddenService.E_ClientCookie)
						if err == nil {
							clientCookieB := [16]byte{}
							copy(clientCookieB[:], clientCookie.([]byte)[:])
							handler.OnAuthenticationRequest(opm.GetChannelIdentifier(), clientCookieB)
						} else {
							// Must include Client Cookie
							handler.OnBadUsageError(opm.GetChannelIdentifier())
						}
					}
				case "im.ricochet.chat":
					if !oc.IsAuthed {
						// Can't open chat channel if not authorized
						handler.OnUnauthorizedError(opm.GetChannelIdentifier())
					} else if !handler.IsKnownContact(oc.OtherHostname) {
						// Can't open chat channel if not a known contact
						handler.OnUnauthorizedError(opm.GetChannelIdentifier())
					} else {
						handler.OnOpenChannelRequest(opm.GetChannelIdentifier(), "im.ricochet.chat")
					}
				case "im.ricochet.contact.request":
					if oc.Client {
						// Servers are not allowed to send contact requests
						handler.OnBadUsageError(opm.GetChannelIdentifier())
					} else if !oc.IsAuthed {
						// Can't open a contact channel if not authed
						handler.OnUnauthorizedError(opm.GetChannelIdentifier())
					} else if oc.HasChannel("im.ricochet.contact.request") {
						// Only 1 contact channel is allowed to be open at a time
						handler.OnBadUsageError(opm.GetChannelIdentifier())
					} else {
						contactRequestI, err := proto.GetExtension(opm, Protocol_Data_ContactRequest.E_ContactRequest)
						if err == nil {
							contactRequest, check := contactRequestI.(*Protocol_Data_ContactRequest.ContactRequest)
							if check {
								handler.OnContactRequest(opm.GetChannelIdentifier(), contactRequest.GetNickname(), contactRequest.GetMessageText())
								break
							}
						}
						handler.OnBadUsageError(opm.GetChannelIdentifier())
					}
				default:
					handler.OnUnknownTypeError(opm.GetChannelIdentifier())
				}
			} else if res.GetChannelResult() != nil {
				crm := res.GetChannelResult()
				if crm.GetOpened() {
					switch oc.GetChannelType(crm.GetChannelIdentifier()) {
					case "im.ricochet.auth.hidden-service":
						serverCookie, err := proto.GetExtension(crm, Protocol_Data_AuthHiddenService.E_ServerCookie)
						if err == nil {
							serverCookieB := [16]byte{}
							copy(serverCookieB[:], serverCookie.([]byte)[:])
							handler.OnAuthenticationChallenge(crm.GetChannelIdentifier(), serverCookieB)
						} else {
							handler.OnBadUsageError(crm.GetChannelIdentifier())
						}
					case "im.ricochet.chat":
						handler.OnOpenChannelRequestSuccess(crm.GetChannelIdentifier())
					case "im.ricochet.contact.request":
						responseI, err := proto.GetExtension(res.GetChannelResult(), Protocol_Data_ContactRequest.E_Response)
						if err == nil {
							response, check := responseI.(*Protocol_Data_ContactRequest.Response)
							if check {
								handler.OnContactRequestAck(crm.GetChannelIdentifier(), response.GetStatus().String())
								break
							}
						}
						handler.OnBadUsageError(crm.GetChannelIdentifier())
					default:
						handler.OnBadUsageError(crm.GetChannelIdentifier())
					}
				} else {
					if oc.GetChannelType(crm.GetChannelIdentifier()) != "none" {
						handler.OnFailedChannelOpen(crm.GetChannelIdentifier(), crm.GetCommonError().String())
					} else {
						oc.CloseChannel(crm.GetChannelIdentifier())
					}
				}
			} else {
				// Unknown Message
				oc.CloseChannel(packet.Channel)
			}
		} else if oc.GetChannelType(packet.Channel) == "im.ricochet.auth.hidden-service" {
			res := new(Protocol_Data_AuthHiddenService.Packet)
			err := proto.Unmarshal(packet.Data[:], res)

			if err != nil {
				oc.CloseChannel(packet.Channel)
				continue
			}

			if res.GetProof() != nil && !oc.Client { // Only Clients Send Proofs
				handler.OnAuthenticationProof(packet.Channel, res.GetProof().GetPublicKey(), res.GetProof().GetSignature())
			} else if res.GetResult() != nil && oc.Client { // Only Servers Send Results
				handler.OnAuthenticationResult(packet.Channel, res.GetResult().GetAccepted(), res.GetResult().GetIsKnownContact())
			} else {
				// If neither of the above are satisfied we just close the connection
				oc.Close()
			}

		} else if oc.GetChannelType(packet.Channel) == "im.ricochet.chat" {

			// NOTE: These auth checks should be redundant, however they
			// are included here for defense-in-depth if for some reason
			// a previously authed connection becomes untrusted / not known and
			// the state is not cleaned up.
			if !oc.IsAuthed {
				// Can't send chat messages if not authorized
				handler.OnUnauthorizedError(packet.Channel)
			} else if !handler.IsKnownContact(oc.OtherHostname) {
				// Can't send chat message if not a known contact
				handler.OnUnauthorizedError(packet.Channel)
			} else {
				res := new(Protocol_Data_Chat.Packet)
				err := proto.Unmarshal(packet.Data[:], res)

				if err != nil {
					oc.CloseChannel(packet.Channel)
					continue
				}

				if res.GetChatMessage() != nil {
					handler.OnChatMessage(packet.Channel, int32(res.GetChatMessage().GetMessageId()), res.GetChatMessage().GetMessageText())
				} else if res.GetChatAcknowledge() != nil {
					handler.OnChatMessageAck(packet.Channel, int32(res.GetChatMessage().GetMessageId()))
				} else {
					// If neither of the above are satisfied we just close the connection
					oc.Close()
				}
			}
		} else if oc.GetChannelType(packet.Channel) == "im.ricochet.contact.request" {

			// NOTE: These auth checks should be redundant, however they
			// are included here for defense-in-depth if for some reason
			// a previously authed connection becomes untrusted / not known and
			// the state is not cleaned up.
			if !oc.Client {
				// Clients are not allowed to send contact request responses
				handler.OnBadUsageError(packet.Channel)
			} else if !oc.IsAuthed {
				// Can't send a contact request if not authed
				handler.OnBadUsageError(packet.Channel)
			} else {
				res := new(Protocol_Data_ContactRequest.Response)
				err := proto.Unmarshal(packet.Data[:], res)
				log.Printf("%v", res)
				if err != nil {
					oc.CloseChannel(packet.Channel)
					continue
				}
				handler.OnContactRequestAck(packet.Channel, res.GetStatus().String())
			}
		} else if oc.GetChannelType(packet.Channel) == "none" {
			// Invalid Channel Assignment
			oc.CloseChannel(packet.Channel)
		} else {
			oc.Close()
		}
	}
}
