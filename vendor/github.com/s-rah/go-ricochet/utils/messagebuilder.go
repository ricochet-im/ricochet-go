package utils

import (
	"github.com/golang/protobuf/proto"
	"github.com/s-rah/go-ricochet/wire/auth"
	"github.com/s-rah/go-ricochet/wire/chat"
	"github.com/s-rah/go-ricochet/wire/contact"
	"github.com/s-rah/go-ricochet/wire/control"
)

// MessageBuilder allows a client to construct specific data packets for the
// ricochet protocol.
type MessageBuilder struct {
}

// OpenChannel contructs a message which will request to open a channel for
// chat on the given channelID.
func (mb *MessageBuilder) OpenChannel(channelID int32, channelType string) []byte {
	oc := &Protocol_Data_Control.OpenChannel{
		ChannelIdentifier: proto.Int32(channelID),
		ChannelType:       proto.String(channelType),
	}
	pc := &Protocol_Data_Control.Packet{
		OpenChannel: oc,
	}
	ret, err := proto.Marshal(pc)
	CheckError(err)
	return ret
}

// AckOpenChannel constructs a message to acknowledge a previous open channel operation.
func (mb *MessageBuilder) AckOpenChannel(channelID int32) []byte {
	cr := &Protocol_Data_Control.ChannelResult{
		ChannelIdentifier: proto.Int32(channelID),
		Opened:            proto.Bool(true),
	}
	pc := &Protocol_Data_Control.Packet{
		ChannelResult: cr,
	}
	ret, err := proto.Marshal(pc)
	CheckError(err)
	return ret
}

// RejectOpenChannel constructs a channel result message, stating the channel failed to open and a reason
func (mb *MessageBuilder) RejectOpenChannel(channelID int32, error string) []byte {

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
	ret, err := proto.Marshal(pc)
	CheckError(err)
	return ret
}

// ConfirmAuthChannel constructs a message to acknowledge a previous open channel operation.
func (mb *MessageBuilder) ConfirmAuthChannel(channelID int32, serverCookie [16]byte) []byte {
	cr := &Protocol_Data_Control.ChannelResult{
		ChannelIdentifier: proto.Int32(channelID),
		Opened:            proto.Bool(true),
	}

	err := proto.SetExtension(cr, Protocol_Data_AuthHiddenService.E_ServerCookie, serverCookie[:])
	CheckError(err)

	pc := &Protocol_Data_Control.Packet{
		ChannelResult: cr,
	}
	ret, err := proto.Marshal(pc)
	CheckError(err)
	return ret
}

// OpenContactRequestChannel contructs a message which will reuqest to open a channel for
// a contact request on the given channelID, with the given nick and message.
func (mb *MessageBuilder) OpenContactRequestChannel(channelID int32, nick string, message string) []byte {
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
	CheckError(err)

	pc := &Protocol_Data_Control.Packet{
		OpenChannel: oc,
	}
	ret, err := proto.Marshal(pc)
	CheckError(err)
	return ret
}

// ReplyToContactRequestOnResponse constructs a message to acknowledge contact request
func (mb *MessageBuilder) ReplyToContactRequestOnResponse(channelID int32, status string) []byte {
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
	CheckError(err)

	pc := &Protocol_Data_Control.Packet{
		ChannelResult: cr,
	}
	ret, err := proto.Marshal(pc)
	CheckError(err)
	return ret
}

// ReplyToContactRequest constructs a message to acknowledge a contact request
func (mb *MessageBuilder) ReplyToContactRequest(channelID int32, status string) []byte {
	statusNum := Protocol_Data_ContactRequest.Response_Status_value[status]
	responseStatus := Protocol_Data_ContactRequest.Response_Status(statusNum)
	contactRequest := &Protocol_Data_ContactRequest.Response{
		Status: &responseStatus,
	}

	ret, err := proto.Marshal(contactRequest)
	CheckError(err)
	return ret
}

// OpenAuthenticationChannel constructs a message which will reuqest to open a channel for
// authentication on the given channelID, with the given cookie
func (mb *MessageBuilder) OpenAuthenticationChannel(channelID int32, clientCookie [16]byte) []byte {
	oc := &Protocol_Data_Control.OpenChannel{
		ChannelIdentifier: proto.Int32(channelID),
		ChannelType:       proto.String("im.ricochet.auth.hidden-service"),
	}
	err := proto.SetExtension(oc, Protocol_Data_AuthHiddenService.E_ClientCookie, clientCookie[:])
	CheckError(err)

	pc := &Protocol_Data_Control.Packet{
		OpenChannel: oc,
	}
	ret, err := proto.Marshal(pc)
	CheckError(err)
	return ret
}

// Proof constructs a proof message with the given public key and signature.
func (mb *MessageBuilder) Proof(publicKeyBytes []byte, signatureBytes []byte) []byte {
	proof := &Protocol_Data_AuthHiddenService.Proof{
		PublicKey: publicKeyBytes,
		Signature: signatureBytes,
	}

	ahsPacket := &Protocol_Data_AuthHiddenService.Packet{
		Proof:  proof,
		Result: nil,
	}

	ret, err := proto.Marshal(ahsPacket)
	CheckError(err)
	return ret
}

// AuthResult constructs a response to a Proof
func (mb *MessageBuilder) AuthResult(accepted bool, isKnownContact bool) []byte {
	// Construct a Result Message
	result := &Protocol_Data_AuthHiddenService.Result{
		Accepted:       proto.Bool(accepted),
		IsKnownContact: proto.Bool(isKnownContact),
	}

	ahsPacket := &Protocol_Data_AuthHiddenService.Packet{
		Proof:  nil,
		Result: result,
	}

	ret, err := proto.Marshal(ahsPacket)
	CheckError(err)
	return ret
}

// ChatMessage constructs a chat message with the given content.
func (mb *MessageBuilder) ChatMessage(message string, messageID uint32, timeDelta int64) []byte {
	cm := &Protocol_Data_Chat.ChatMessage{
		MessageId:   proto.Uint32(messageID),
		MessageText: proto.String(message),
		TimeDelta:   proto.Int64(timeDelta),
	}
	chatPacket := &Protocol_Data_Chat.Packet{
		ChatMessage: cm,
	}
	ret, err := proto.Marshal(chatPacket)
	CheckError(err)
	return ret
}

// AckChatMessage constructs a chat message acknowledgement.
func (mb *MessageBuilder) AckChatMessage(messageID uint32, accepted bool) []byte {
	cr := &Protocol_Data_Chat.ChatAcknowledge{
		MessageId: proto.Uint32(messageID),
		Accepted:  proto.Bool(accepted),
	}
	pc := &Protocol_Data_Chat.Packet{
		ChatAcknowledge: cr,
	}
	ret, err := proto.Marshal(pc)
	CheckError(err)
	return ret
}

// KeepAlive ...
func (mb *MessageBuilder) KeepAlive(responseRequested bool) []byte {
	ka := &Protocol_Data_Control.KeepAlive{
		ResponseRequested: proto.Bool(responseRequested),
	}
	pc := &Protocol_Data_Control.Packet{
		KeepAlive: ka,
	}
	ret, err := proto.Marshal(pc)
	CheckError(err)
	return ret
}

// EnableFeatures ...
func (mb *MessageBuilder) EnableFeatures(features []string) []byte {
	ef := &Protocol_Data_Control.EnableFeatures{
		Feature: features,
	}
	pc := &Protocol_Data_Control.Packet{
		EnableFeatures: ef,
	}
	ret, err := proto.Marshal(pc)
	CheckError(err)
	return ret
}

// FeaturesEnabled ...
func (mb *MessageBuilder) FeaturesEnabled(features []string) []byte {
	fe := &Protocol_Data_Control.FeaturesEnabled{
		Feature: features,
	}
	pc := &Protocol_Data_Control.Packet{
		FeaturesEnabled: fe,
	}
	ret, err := proto.Marshal(pc)
	CheckError(err)
	return ret
}
