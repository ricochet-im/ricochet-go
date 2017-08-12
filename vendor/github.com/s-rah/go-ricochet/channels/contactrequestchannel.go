package channels

import (
	"github.com/golang/protobuf/proto"
	"github.com/s-rah/go-ricochet/utils"
	"github.com/s-rah/go-ricochet/wire/contact"
	"github.com/s-rah/go-ricochet/wire/control"
)

// Defining Versions
const (
	InvalidContactNameError    = utils.Error("InvalidContactNameError")
	InvalidContactMessageError = utils.Error("InvalidContactMessageError")
	InvalidContactRequestError = utils.Error("InvalidContactRequestError")
)

// ContactRequestChannel implements the ChannelHandler interface for a channel of
// type "im.ricochet.contact.request". The channel may be inbound or outbound.
type ContactRequestChannel struct {
	// Methods of Handler are called for chat events on this channel
	Handler ContactRequestChannelHandler
	channel *Channel

	// Properties of the request
	Name    string
	Message string
}

// ContactRequestChannelHandler is implemented by an application type to receive
// events from a ContactRequestChannel.
//
// Note that ContactRequestChannelHandler is composable with other interfaces, including
// ConnectionHandler; there is no need to use a distinct type as a
// ContactRequestChannelHandler.
type ContactRequestChannelHandler interface {
	ContactRequest(name string, message string) string
	ContactRequestRejected()
	ContactRequestAccepted()
	ContactRequestError()
}

// OnlyClientCanOpen - only clients can open contact requests
func (crc *ContactRequestChannel) OnlyClientCanOpen() bool {
	return true
}

// Singleton - only one contact request can be opened per side
func (crc *ContactRequestChannel) Singleton() bool {
	return true
}

// Bidirectional - only clients can send messages
func (crc *ContactRequestChannel) Bidirectional() bool {
	return false
}

// RequiresAuthentication - contact requests require hidden service auth
func (crc *ContactRequestChannel) RequiresAuthentication() string {
	return "im.ricochet.auth.hidden-service"
}

// Type returns the type string for this channel, e.g. "im.ricochet.chat".
func (crc *ContactRequestChannel) Type() string {
	return "im.ricochet.contact.request"
}

// Closed is called when the channel is closed for any reason.
func (crc *ContactRequestChannel) Closed(err error) {

}

// OpenInbound is the first method called for an inbound channel request.
// If an error is returned, the channel is rejected. If a RawMessage is
// returned, it will be sent as the ChannelResult message.
func (crc *ContactRequestChannel) OpenInbound(channel *Channel, oc *Protocol_Data_Control.OpenChannel) ([]byte, error) {
	crc.channel = channel
	contactRequestI, err := proto.GetExtension(oc, Protocol_Data_ContactRequest.E_ContactRequest)
	if err == nil {
		contactRequest, check := contactRequestI.(*Protocol_Data_ContactRequest.ContactRequest)
		if check {

			if len(contactRequest.GetNickname()) > int(Protocol_Data_ContactRequest.Limits_NicknameMaxCharacters) {
				// Violation of the Protocol
				return nil, InvalidContactNameError
			}

			if len(contactRequest.GetMessageText()) > int(Protocol_Data_ContactRequest.Limits_MessageMaxCharacters) {
				// Violation of the Protocol
				return nil, InvalidContactMessageError
			}

			crc.Name = contactRequest.GetNickname()
			crc.Message = contactRequest.GetMessageText()
			result := crc.Handler.ContactRequest(contactRequest.GetNickname(), contactRequest.GetMessageText())
			messageBuilder := new(utils.MessageBuilder)
			return messageBuilder.ReplyToContactRequestOnResponse(channel.ID, result), nil
		}
	}
	return nil, InvalidContactRequestError
}

// OpenOutbound is the first method called for an outbound channel request.
// If an error is returned, the channel is not opened. If a RawMessage is
// returned, it will be sent as the OpenChannel message.
func (crc *ContactRequestChannel) OpenOutbound(channel *Channel) ([]byte, error) {
	crc.channel = channel
	messageBuilder := new(utils.MessageBuilder)
	return messageBuilder.OpenContactRequestChannel(channel.ID, crc.Name, crc.Message), nil
}

// OpenOutboundResult is called when a response is received for an
// outbound OpenChannel request. If `err` is non-nil, the channel was
// rejected and Closed will be called immediately afterwards. `raw`
// contains the raw protocol message including any extension data.
func (crc *ContactRequestChannel) OpenOutboundResult(err error, crm *Protocol_Data_Control.ChannelResult) {
	if err == nil {
		if crm.GetOpened() {
			responseI, err := proto.GetExtension(crm, Protocol_Data_ContactRequest.E_Response)
			if err == nil {
				response, check := responseI.(*Protocol_Data_ContactRequest.Response)
				if check {
					crc.handleStatus(response.GetStatus().String())
					return
				}
			}
		}
	}
	crc.channel.SendMessage([]byte{})
}

func (crc *ContactRequestChannel) SendResponse(status string) {
	messageBuilder := new(utils.MessageBuilder)
	crc.channel.SendMessage(messageBuilder.ReplyToContactRequest(crc.channel.ID, status))
}

func (crc *ContactRequestChannel) handleStatus(status string) {
	switch status {
	case "Accepted":
		crc.Handler.ContactRequestAccepted()
	case "Pending":
		break
	case "Rejected":
		crc.Handler.ContactRequestRejected()
		break
	case "Error":
		crc.Handler.ContactRequestError()
		break
	}
}

// Packet is called for each raw packet received on this channel.
func (crc *ContactRequestChannel) Packet(data []byte) {
	if !crc.channel.Pending {
		response := new(Protocol_Data_ContactRequest.Response)
		err := proto.Unmarshal(data, response)
		if err == nil {
			crc.handleStatus(response.GetStatus().String())
			return
		}
	}
	crc.channel.SendMessage([]byte{})
}
