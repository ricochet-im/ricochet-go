package channels

import (
	"crypto/rand"
	"github.com/golang/protobuf/proto"
	"github.com/s-rah/go-ricochet/utils"
	"github.com/s-rah/go-ricochet/wire/chat"
	"github.com/s-rah/go-ricochet/wire/control"
	"math"
	"math/big"
	"time"
)

// ChatChannel implements the ChannelHandler interface for a channel of
// type "im.ricochet.chat". The channel may be inbound or outbound.
//
// ChatChannel implements protocol-level sanity and state validation, but
// does not handle or acknowledge chat messages. The application must provide
// a ChatChannelHandler implementation to handle chat events.
type ChatChannel struct {
	// Methods of Handler are called for chat events on this channel
	Handler       ChatChannelHandler
	channel       *Channel
	lastMessageID uint32
}

// ChatChannelHandler is implemented by an application type to receive
// events from a ChatChannel.
//
// Note that ChatChannelHandler is composable with other interfaces, including
// ConnectionHandler; there is no need to use a distinct type as a
// ChatChannelHandler.
type ChatChannelHandler interface {
	// ChatMessage is called when a chat message is received. Return true to acknowledge
	// the message successfully, and false to NACK and refuse the message.
	ChatMessage(messageID uint32, when time.Time, message string) bool
	// ChatMessageAck is called when an acknowledgement of a sent message is received.
	ChatMessageAck(messageID uint32, accepted bool)
}

// SendMessage sends a given message using this channel, and returns the
// messageID, which will be used in ChatMessageAck when the peer acknowledges
// this message.
func (cc *ChatChannel) SendMessage(message string) uint32 {
	return cc.SendMessageWithTime(message, time.Now())
}

// SendMessageWithTime is identical to SendMessage, but also sends the provided time.Time
// as a rough timestamp for when this message was originally sent. This should be used
// when retrying or sending queued messages.
func (cc *ChatChannel) SendMessageWithTime(message string, when time.Time) uint32 {
	delta := time.Now().Sub(when) / time.Second
	messageBuilder := new(utils.MessageBuilder)
	messageID := cc.lastMessageID
	cc.lastMessageID++
	data := messageBuilder.ChatMessage(message, messageID, int64(delta))
	cc.channel.SendMessage(data)
	return messageID
}

// Acknowledge indicates that the given messageID was received, and whether
// it was accepted.
func (cc *ChatChannel) Acknowledge(messageID uint32, accepted bool) {
	messageBuilder := new(utils.MessageBuilder)
	cc.channel.SendMessage(messageBuilder.AckChatMessage(messageID, accepted))
}

// Type returns the type string for this channel, e.g. "im.ricochet.chat".
func (cc *ChatChannel) Type() string {
	return "im.ricochet.chat"
}

// Closed is called when the channel is closed for any reason.
func (cc *ChatChannel) Closed(err error) {

}

// OnlyClientCanOpen  - for chat channels any side can open
func (cc *ChatChannel) OnlyClientCanOpen() bool {
	return false
}

// Singleton - for chat channels there can only be one instance per direction
func (cc *ChatChannel) Singleton() bool {
	return true
}

// Bidirectional - for chat channels are not bidrectional
func (cc *ChatChannel) Bidirectional() bool {
	return false
}

// RequiresAuthentication - chat channels require hidden service auth
func (cc *ChatChannel) RequiresAuthentication() string {
	return "im.ricochet.auth.hidden-service"
}

// OpenInbound is the first method called for an inbound channel request.
// If an error is returned, the channel is rejected. If a RawMessage is
// returned, it will be sent as the ChannelResult message.
func (cc *ChatChannel) OpenInbound(channel *Channel, raw *Protocol_Data_Control.OpenChannel) ([]byte, error) {
	cc.channel = channel
	id, err := rand.Int(rand.Reader, big.NewInt(math.MaxUint32))
	if err != nil {
		return nil, err
	}
	cc.lastMessageID = uint32(id.Uint64())
	cc.channel.Pending = false
	messageBuilder := new(utils.MessageBuilder)
	return messageBuilder.AckOpenChannel(channel.ID), nil
}

// OpenOutbound is the first method called for an outbound channel request.
// If an error is returned, the channel is not opened. If a RawMessage is
// returned, it will be sent as the OpenChannel message.
func (cc *ChatChannel) OpenOutbound(channel *Channel) ([]byte, error) {
	cc.channel = channel
	id, err := rand.Int(rand.Reader, big.NewInt(math.MaxUint32))
	if err != nil {
		return nil, err
	}
	cc.lastMessageID = uint32(id.Uint64())
	messageBuilder := new(utils.MessageBuilder)
	return messageBuilder.OpenChannel(channel.ID, cc.Type()), nil
}

// OpenOutboundResult is called when a response is received for an
// outbound OpenChannel request. If `err` is non-nil, the channel was
// rejected and Closed will be called immediately afterwards. `raw`
// contains the raw protocol message including any extension data.
func (cc *ChatChannel) OpenOutboundResult(err error, crm *Protocol_Data_Control.ChannelResult) {
	if err == nil {
		if crm.GetOpened() {
			cc.channel.Pending = false
		}
	}
}

// Packet is called for each raw packet received on this channel.
func (cc *ChatChannel) Packet(data []byte) {
	if !cc.channel.Pending {
		res := new(Protocol_Data_Chat.Packet)
		err := proto.Unmarshal(data, res)
		if err == nil {
			if res.GetChatMessage() != nil {
				ack := cc.Handler.ChatMessage(res.GetChatMessage().GetMessageId(), time.Now(), res.GetChatMessage().GetMessageText())
				cc.Acknowledge(res.GetChatMessage().GetMessageId(), ack)
			} else if ack := res.GetChatAcknowledge(); ack != nil {
				cc.Handler.ChatMessageAck(ack.GetMessageId(), ack.GetAccepted())
			}
			// XXX?
		}
	}
}
