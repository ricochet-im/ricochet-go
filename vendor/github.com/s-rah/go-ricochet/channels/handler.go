package channels

import (
	"github.com/s-rah/go-ricochet/wire/control"
)

// Handler reacts to low-level events on a protocol channel. There
// should be a unique instance of a ChannelHandler type per channel.
//
// Applications generally don't need to implement ChannelHandler directly;
// instead, use the built-in implementations for common channel types, and
// their individual callback interfaces. ChannelHandler is useful when
// implementing new channel types, or modifying low level default behavior.
type Handler interface {
	// Type returns the type string for this channel, e.g. "im.ricochet.chat".
	Type() string

	// Closed is called when the channel is closed for any reason.
	Closed(err error)

	// OnlyClientCanOpen indicates if only a client can open a given channel
	OnlyClientCanOpen() bool

	// Singleton indicates if a channel can only have one instance per direction
	Singleton() bool

	// Bidirectional indicates if messages can be send by either side
	Bidirectional() bool

	// RequiresAuthentication describes what authentication is needed for the channel
	RequiresAuthentication() string

	// OpenInbound is the first method called for an inbound channel request.
	// If an error is returned, the channel is rejected. If a RawMessage is
	// returned, it will be sent as the ChannelResult message.
	OpenInbound(channel *Channel, raw *Protocol_Data_Control.OpenChannel) ([]byte, error)

	// OpenOutbound is the first method called for an outbound channel request.
	// If an error is returned, the channel is not opened. If a RawMessage is
	// returned, it will be sent as the OpenChannel message.
	OpenOutbound(channel *Channel) ([]byte, error)

	// OpenOutboundResult is called when a response is received for an
	// outbound OpenChannel request. If `err` is non-nil, the channel was
	// rejected and Closed will be called immediately afterwards. `raw`
	// contains the raw protocol message including any extension data.
	OpenOutboundResult(err error, raw *Protocol_Data_Control.ChannelResult)

	// Packet is called for each raw packet received on this channel.
	Packet(data []byte)
}
