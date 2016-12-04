package goricochet

import (
	"github.com/golang/protobuf/proto"
	"github.com/s-rah/go-ricochet/auth"
	"github.com/s-rah/go-ricochet/chat"
	"github.com/s-rah/go-ricochet/contact"
	"github.com/s-rah/go-ricochet/control"
	"github.com/s-rah/go-ricochet/utils"
)

// MessageBuilder allows a client to construct specific data packets for the
// ricochet protocol.
type MessageBuilder struct {
}

// OpenChannel contructs a message which will request to open a channel for
// chat on the given channelID.
func (mb *MessageBuilder) OpenChannel(channelID int32, channelType string) ([]byte, error) {
	oc := &Protocol_Data_Control.OpenChannel{
		ChannelIdentifier: proto.Int32(channelID),
		ChannelType:       proto.String(channelType),
	}
	pc := &Protocol_Data_Control.Packet{
		OpenChannel: oc,
	}
	return proto.Marshal(pc)
}

// AckOpenChannel constructs a message to acknowledge a previous open channel operation.
func (mb *MessageBuilder) AckOpenChannel(channelID int32) ([]byte, error) {
	cr := &Protocol_Data_Control.ChannelResult{
		ChannelIdentifier: proto.Int32(channelID),
		Opened:            proto.Bool(true),
	}
	pc := &Protocol_Data_Control.Packet{
		ChannelResult: cr,
	}
	return proto.Marshal(pc)
}

// RejectOpenChannel constructs a channel result message, stating the channel failed to open and a reason
func (mb *MessageBuilder) RejectOpenChannel(channelID int32, error string) ([]byte, error) {

	errorNum := Protocol_Data_Control.ChannelResult_CommonError_value[error]
	commonError := Protocol_Data_Control.ChannelResult_CommonError(errorNum)

	cr := &Protocol_Data_Control.ChannelResult{
		ChannelIdentifier: proto.Int32(channelID),
		Opened:            proto.Bool(false),
		CommonError:       &commonError,
	}
	pc := &Protocol_Data_Control.Packet{
		ChannelResult: cr,
	}
	return proto.Marshal(pc)
}

// ConfirmAuthChannel constructs a message to acknowledge a previous open channel operation.
func (mb *MessageBuilder) ConfirmAuthChannel(channelID int32, serverCookie [16]byte) ([]byte, error) {
	cr := &Protocol_Data_Control.ChannelResult{
		ChannelIdentifier: proto.Int32(channelID),
		Opened:            proto.Bool(true),
	}

	err := proto.SetExtension(cr, Protocol_Data_AuthHiddenService.E_ServerCookie, serverCookie[:])
	utils.CheckError(err)

	pc := &Protocol_Data_Control.Packet{
		ChannelResult: cr,
	}
	return proto.Marshal(pc)
}

// OpenContactRequestChannel contructs a message which will reuqest to open a channel for
// a contact request on the given channelID, with the given nick and message.
func (mb *MessageBuilder) OpenContactRequestChannel(channelID int32, nick string, message string) ([]byte, error) {
	// Construct a Contact Request Channel
	oc := &Protocol_Data_Control.OpenChannel{
		ChannelIdentifier: proto.Int32(channelID),
		ChannelType:       proto.String("im.ricochet.contact.request"),
	}

	contactRequest := &Protocol_Data_ContactRequest.ContactRequest{
		Nickname:    proto.String(nick),
		MessageText: proto.String(message),
	}

	err := proto.SetExtension(oc, Protocol_Data_ContactRequest.E_ContactRequest, contactRequest)
	utils.CheckError(err)

	pc := &Protocol_Data_Control.Packet{
		OpenChannel: oc,
	}
	return proto.Marshal(pc)
}

// ReplyToContactRequestOnResponse constructs a message to acknowledge contact request
func (mb *MessageBuilder) ReplyToContactRequestOnResponse(channelID int32, status string) ([]byte, error) {
	cr := &Protocol_Data_Control.ChannelResult{
		ChannelIdentifier: proto.Int32(channelID),
		Opened:            proto.Bool(true),
	}

	statusNum := Protocol_Data_ContactRequest.Response_Status_value[status]
	responseStatus := Protocol_Data_ContactRequest.Response_Status(statusNum)
	contactRequest := &Protocol_Data_ContactRequest.Response{
		Status: &responseStatus,
	}

	err := proto.SetExtension(cr, Protocol_Data_ContactRequest.E_Response, contactRequest)
	utils.CheckError(err)

	pc := &Protocol_Data_Control.Packet{
		ChannelResult: cr,
	}
	return proto.Marshal(pc)
}

// ReplyToContactRequest constructs a message to acknowledge a contact request
func (mb *MessageBuilder) ReplyToContactRequest(channelID int32, status string) ([]byte, error) {
	statusNum := Protocol_Data_ContactRequest.Response_Status_value[status]
	responseStatus := Protocol_Data_ContactRequest.Response_Status(statusNum)
	contactRequest := &Protocol_Data_ContactRequest.Response{
		Status: &responseStatus,
	}
	return proto.Marshal(contactRequest)
}

// OpenAuthenticationChannel constructs a message which will reuqest to open a channel for
// authentication on the given channelID, with the given cookie
func (mb *MessageBuilder) OpenAuthenticationChannel(channelID int32, clientCookie [16]byte) ([]byte, error) {
	oc := &Protocol_Data_Control.OpenChannel{
		ChannelIdentifier: proto.Int32(channelID),
		ChannelType:       proto.String("im.ricochet.auth.hidden-service"),
	}
	err := proto.SetExtension(oc, Protocol_Data_AuthHiddenService.E_ClientCookie, clientCookie[:])
	utils.CheckError(err)

	pc := &Protocol_Data_Control.Packet{
		OpenChannel: oc,
	}
	return proto.Marshal(pc)
}

// Proof constructs a proof message with the given public key and signature.
func (mb *MessageBuilder) Proof(publicKeyBytes []byte, signatureBytes []byte) ([]byte, error) {
	proof := &Protocol_Data_AuthHiddenService.Proof{
		PublicKey: publicKeyBytes,
		Signature: signatureBytes,
	}

	ahsPacket := &Protocol_Data_AuthHiddenService.Packet{
		Proof:  proof,
		Result: nil,
	}

	return proto.Marshal(ahsPacket)
}

// AuthResult constructs a response to a Proof
func (mb *MessageBuilder) AuthResult(accepted bool, isKnownContact bool) ([]byte, error) {
	// Construct a Result Message
	result := &Protocol_Data_AuthHiddenService.Result{
		Accepted:       proto.Bool(accepted),
		IsKnownContact: proto.Bool(isKnownContact),
	}

	ahsPacket := &Protocol_Data_AuthHiddenService.Packet{
		Proof:  nil,
		Result: result,
	}

	return proto.Marshal(ahsPacket)
}

// ChatMessage constructs a chat message with the given content.
func (mb *MessageBuilder) ChatMessage(message string, messageID int32) ([]byte, error) {
	cm := &Protocol_Data_Chat.ChatMessage{
		MessageId:   proto.Uint32(uint32(messageID)),
		MessageText: proto.String(message),
	}
	chatPacket := &Protocol_Data_Chat.Packet{
		ChatMessage: cm,
	}
	return proto.Marshal(chatPacket)
}

// AckChatMessage constructs a chat message acknowledgement.
func (mb *MessageBuilder) AckChatMessage(messageID int32) ([]byte, error) {
	cr := &Protocol_Data_Chat.ChatAcknowledge{
		MessageId: proto.Uint32(uint32(messageID)),
		Accepted:  proto.Bool(true),
	}
	pc := &Protocol_Data_Chat.Packet{
		ChatAcknowledge: cr,
	}
	return proto.Marshal(pc)
}
