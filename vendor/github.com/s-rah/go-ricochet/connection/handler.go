package connection

import (
	"github.com/s-rah/go-ricochet/channels"
)

// Handler reacts to low-level events on a protocol connection.
// There should be a unique instance of a ConnectionHandler type per
// OpenConnection.
type Handler interface {
	// OnReady is called when the connection begins using this handler.
	OnReady(oc *Connection)

	// OnClosed is called when the OpenConnection has closed for any reason.
	OnClosed(err error)

	// OpenChannelRequest is called when the peer asks to open a channel of
	// `type`. `raw` contains the protocol OpenChannel message including any
	// extension data. If this channel type is recognized and allowed by this
	// connection in this state, return a type implementing ChannelHandler for
	// events related to this channel. Returning an error or nil rejects the
	// channel.
	//
	// Channel type handlers may implement additional state and sanity checks.
	// A non-nil return from this function does not guarantee that the channel
	// will be opened.
	OnOpenChannelRequest(ctype string) (channels.Handler, error)
}
