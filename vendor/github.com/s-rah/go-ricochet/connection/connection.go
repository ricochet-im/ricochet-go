package connection

import (
	"context"
	"errors"
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/s-rah/go-ricochet/channels"
	"github.com/s-rah/go-ricochet/utils"
	"github.com/s-rah/go-ricochet/wire/control"
	"io"
	"log"
	"sync"
)

// Connection encapsulates the state required to maintain a connection to
// a ricochet service.
type Connection struct {
	utils.RicochetNetwork

	channelManager *ChannelManager

	// Ricochet Network Loop
	packetChannel chan utils.RicochetData
	errorChannel  chan error

	breakChannel       chan bool
	breakResultChannel chan error

	unlockChannel         chan bool
	unlockResponseChannel chan bool

	messageBuilder utils.MessageBuilder
	trace          bool

	closed  bool
	closing bool
	// This mutex is exclusively for preventing races during blocking
	// interactions with Process; specifically Do and Break. Don't use
	// it for anything else. See those functions for an explanation.
	processBlockMutex sync.Mutex

	Conn           io.ReadWriteCloser
	IsInbound      bool
	Authentication map[string]bool
	RemoteHostname string
}

func (rc *Connection) init() {

	rc.packetChannel = make(chan utils.RicochetData)
	rc.errorChannel = make(chan error)

	rc.breakChannel = make(chan bool)
	rc.breakResultChannel = make(chan error)

	rc.unlockChannel = make(chan bool)
	rc.unlockResponseChannel = make(chan bool)

	rc.Authentication = make(map[string]bool)
	go rc.start()
}

// NewInboundConnection creates a new Connection struct
// modelling an Inbound Connection
func NewInboundConnection(conn io.ReadWriteCloser) *Connection {
	rc := new(Connection)
	rc.Conn = conn
	rc.IsInbound = true
	rc.init()
	rc.channelManager = NewServerChannelManager()
	return rc
}

// NewOutboundConnection creates a new Connection struct
// modelling an Inbound Connection
func NewOutboundConnection(conn io.ReadWriteCloser, remoteHostname string) *Connection {
	rc := new(Connection)
	rc.Conn = conn
	rc.IsInbound = false
	rc.init()
	rc.RemoteHostname = remoteHostname
	rc.channelManager = NewClientChannelManager()
	return rc
}

func (rc *Connection) TraceLog(enabled bool) {
	rc.trace = enabled
}

// start
func (rc *Connection) start() {
	for {
		packet, err := rc.RecvRicochetPacket(rc.Conn)
		if err != nil {
			rc.errorChannel <- err
			return
		}
		rc.packetChannel <- packet
	}
}

// Do allows any function utilizing Connection to be run safely, if you're
// careful. All operations which require access (directly or indirectly) to
// Connection while Process is running need to use Do. Calls to Do without
// Process running will block unless the connection is closed, which is
// returned as ConnectionClosedError.
//
// Like a mutex, Do cannot be called recursively. This will deadlock. As
// a result, no API in this library that can be reached from the application
// should use Do, with few exceptions. This would make the API impossible
// to use safely in many cases.
//
// Do is safe to call from methods of connection.Handler and channel.Handler
// that are called by Process.
func (rc *Connection) Do(do func() error) error {
	// There's a complicated little dance here to prevent a race when the
	// Process call is returning for a connection error. The problem is
	// that if Do simply checked rc.closed and then tried to send, it's
	// possible for Process to change rc.closed and stop reading before the
	// send statement is executed, creating a deadlock.
	//
	// To prevent this, all of the functions that block on Process should
	// do so by acquiring processBlockMutex, aborting if rc.closed is true,
	// performing their blocking channel operations, and then releasing the
	// mutex.
	//
	// This works because Process will always use a separate goroutine to
	// acquire processBlockMutex before changing rc.closed, and the mutex
	// guarantees that no blocking channel operation can happen during or
	// after the value is changed. Since these operations block the Process
	// loop, the behavior of multiple concurrent calls to Do/Break doesn't
	// change: they just end up blocking on the mutex before blocking on the
	// channel.
	rc.processBlockMutex.Lock()
	defer rc.processBlockMutex.Unlock()
	if rc.closed {
		return utils.ConnectionClosedError
	}

	// Force process to soft-break so we can lock
	rc.traceLog("request unlocking of process loop for do()")
	rc.unlockChannel <- true
	rc.traceLog("process loop is unlocked for do()")
	defer func() {
		rc.traceLog("giving up lock process loop after do() ")
		rc.unlockResponseChannel <- true
	}()

	// Process sets rc.closing when it's trying to acquire the mutex and
	// close down the connection. Behave as if the connection was already
	// closed.
	if rc.closing {
		return utils.ConnectionClosedError
	}
	return do()
}

// DoContext behaves in the same way as Do, but also respects the provided
// context when blocked, and passes the context to the callback function.
//
// DoContext should be used when any call to Do may need to be cancelled
// or timed out.
func (rc *Connection) DoContext(ctx context.Context, do func(context.Context) error) error {
	// .. see above
	rc.processBlockMutex.Lock()
	defer rc.processBlockMutex.Unlock()
	if rc.closed {
		return utils.ConnectionClosedError
	}

	// Force process to soft-break so we can lock
	rc.traceLog("request unlocking of process loop for do()")
	select {
	case rc.unlockChannel <- true:
		break
	case <-ctx.Done():
		rc.traceLog("giving up on unlocking process loop for do() because context cancelled")
		return ctx.Err()
	}

	rc.traceLog("process loop is unlocked for do()")
	defer func() {
		rc.traceLog("giving up lock process loop after do() ")
		rc.unlockResponseChannel <- true
	}()

	if rc.closing {
		return utils.ConnectionClosedError
	}
	return do(ctx)
}

// RequestOpenChannel sends an OpenChannel message to the remote client.
// An error is returned only if the requirements for opening this channel
// are not met on the local side (a nil error return does not mean the
// channel was opened successfully, because channels open asynchronously).
func (rc *Connection) RequestOpenChannel(ctype string, handler channels.Handler) (*channels.Channel, error) {
	rc.traceLog(fmt.Sprintf("requesting open channel of type %s", ctype))

	// Check that we have the authentication already
	if handler.RequiresAuthentication() != "none" {
		// Enforce Authentication Check.
		_, authed := rc.Authentication[handler.RequiresAuthentication()]
		if !authed {
			return nil, utils.UnauthorizedActionError
		}
	}

	channel, err := rc.channelManager.OpenChannelRequest(handler)

	if err != nil {
		rc.traceLog(fmt.Sprintf("failed to request open channel of type %v", err))
		return nil, err
	}

	channel.SendMessage = func(message []byte) {
		rc.SendRicochetPacket(rc.Conn, channel.ID, message)
	}
	channel.DelegateAuthorization = func() {
		rc.Authentication[handler.Type()] = true
	}
	channel.CloseChannel = func() {
		rc.SendRicochetPacket(rc.Conn, channel.ID, []byte{})
		rc.channelManager.RemoveChannel(channel.ID)
	}
	response, err := handler.OpenOutbound(channel)
	if err == nil {
		rc.traceLog(fmt.Sprintf("requested open channel of type %s", ctype))
		rc.SendRicochetPacket(rc.Conn, 0, response)
	} else {
		rc.traceLog(fmt.Sprintf("failed to request open channel of type %v", err))
		rc.channelManager.RemoveChannel(channel.ID)
	}
	return channel, nil
}

// processUserCallback should be used to wrap any calls into handlers or
// application code from the Process goroutine. It handles calls to Do
// from within that code to prevent deadlocks.
func (rc *Connection) processUserCallback(cb func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		cb()
	}()
	for {
		select {
		case <-done:
			return
		case <-rc.unlockChannel:
			<-rc.unlockResponseChannel
		}
	}
}

// Process receives socket and protocol events for the connection. Methods
// of the application-provided `handler` will be called from this goroutine
// for all events.
//
// Process must be running in order to handle any events on the connection,
// including connection close.
//
// Process blocks until the connection is closed or until Break() is called.
// If the connection is closed, a non-nil error is returned.
func (rc *Connection) Process(handler Handler) error {
	if rc.closed {
		return utils.ConnectionClosedError
	}
	rc.traceLog("entering process loop")
	rc.processUserCallback(func() { handler.OnReady(rc) })

	// There are exactly two ways out of this loop: a signal on breakChannel
	// caused by a call to Break, or a connection-fatal error on errorChannel.
	//
	// In the Break case, no particular care is necessary; it is the caller's
	// responsibility to make sure there aren't e.g. concurrent calls to Do.
	//
	// Because connection errors can happen spontaneously, they must carefully
	// prevent concurrent calls to Break or Do that could deadlock when Process
	// returns.
	for {

		var packet utils.RicochetData
		select {
		case <-rc.unlockChannel:
			<-rc.unlockResponseChannel
			continue
		case <-rc.breakChannel:
			rc.traceLog("process has ended after break")
			rc.breakResultChannel <- nil
			return nil
		case packet = <-rc.packetChannel:
			break
		case err := <-rc.errorChannel:
			rc.Conn.Close()
			rc.closing = true

			// In order to safely close down concurrent calls to Do or Break,
			// processBlockMutex must be held before setting rc.closed. That cannot
			// happen in this goroutine, because one of those calls may already hold
			// the mutex and be blocking on a channel send to this method. So the
			// process here is to have a goroutine acquire the lock, set rc.closed, and
			// signal back. Meanwhile, this one keeps handling unlockChannel and
			// breakChannel.
			closedChan := make(chan struct{})
			go func() {
				rc.processBlockMutex.Lock()
				defer rc.processBlockMutex.Unlock()
				rc.closed = true
				close(closedChan)
			}()

			// Keep accepting calls from Do or Break until closedChan signals that they're
			// safely shut down.
		clearLoop:
			for {
				select {
				case <-rc.unlockChannel:
					<-rc.unlockResponseChannel
				case <-rc.breakChannel:
					rc.breakResultChannel <- utils.ConnectionClosedError
				case <-closedChan:
					break clearLoop
				}
			}

			// This is the one case where processUserCallback isn't necessary, because
			// all calls to Do immediately return ConnectionClosedError now.
			handler.OnClosed(err)
			return err
		}

		if packet.Channel == 0 {
			rc.traceLog(fmt.Sprintf("received control packet on channel %d", packet.Channel))
			res := new(Protocol_Data_Control.Packet)
			err := proto.Unmarshal(packet.Data[:], res)
			if err == nil {
				// Wrap controlPacket in processUserCallback, since it calls out in many
				// places, and wrapping the rest is harmless.
				rc.processUserCallback(func() { rc.controlPacket(handler, res) })
			}
		} else {
			// Let's check to see if we have defined this channel.
			channel, found := rc.channelManager.GetChannel(packet.Channel)
			if found {
				if len(packet.Data) == 0 {
					rc.traceLog(fmt.Sprintf("removing channel %d", packet.Channel))
					rc.channelManager.RemoveChannel(packet.Channel)
					rc.processUserCallback(func() { channel.Handler.Closed(utils.ChannelClosedByPeerError) })
				} else {
					rc.traceLog(fmt.Sprintf("received packet on %v channel %d", channel.Handler.Type(), packet.Channel))
					// Send The Ricochet Packet to the Handler
					rc.processUserCallback(func() { channel.Handler.Packet(packet.Data[:]) })
				}
			} else {
				// When a non-zero packet is received for an unknown
				// channel, the recipient responds by closing
				// that channel.
				rc.traceLog(fmt.Sprintf("received packet on unknown channel %d. closing.", packet.Channel))
				if len(packet.Data) != 0 {
					rc.SendRicochetPacket(rc.Conn, packet.Channel, []byte{})
				}
			}
		}
	}
}

func (rc *Connection) controlPacket(handler Handler, res *Protocol_Data_Control.Packet) {

	if res.GetOpenChannel() != nil {

		opm := res.GetOpenChannel()
		chandler, err := handler.OnOpenChannelRequest(opm.GetChannelType())

		if err != nil {

			response := rc.messageBuilder.RejectOpenChannel(opm.GetChannelIdentifier(), "UnknownTypeError")
			rc.SendRicochetPacket(rc.Conn, 0, response)
			return
		}

		// Check that we have the authentication already
		if chandler.RequiresAuthentication() != "none" {
			rc.traceLog(fmt.Sprintf("channel %v requires authorization of type %v", chandler.Type(), chandler.RequiresAuthentication()))
			// Enforce Authentication Check.
			_, authed := rc.Authentication[chandler.RequiresAuthentication()]
			if !authed {
				rc.SendRicochetPacket(rc.Conn, 0, []byte{})
				rc.traceLog(fmt.Sprintf("do not have required authorization to open channel type %v", chandler.Type()))
				return
			}
			rc.traceLog("succeeded authorization check")
		}

		channel, err := rc.channelManager.OpenChannelRequestFromPeer(opm.GetChannelIdentifier(), chandler)

		if err == nil {

			channel.SendMessage = func(message []byte) {
				rc.SendRicochetPacket(rc.Conn, channel.ID, message)
			}
			channel.DelegateAuthorization = func() {
				rc.Authentication[chandler.Type()] = true
			}
			channel.CloseChannel = func() {
				rc.SendRicochetPacket(rc.Conn, channel.ID, []byte{})
				rc.channelManager.RemoveChannel(channel.ID)
			}

			response, err := chandler.OpenInbound(channel, opm)
			if err == nil && channel.Pending == false {
				rc.traceLog(fmt.Sprintf("opening channel %v on %v", channel.Type, channel.ID))
				rc.SendRicochetPacket(rc.Conn, 0, response)
			} else {
				rc.traceLog(fmt.Sprintf("removing channel %v", channel.ID))
				rc.channelManager.RemoveChannel(channel.ID)
				rc.SendRicochetPacket(rc.Conn, 0, []byte{})
			}
		} else {
			// Send Error Packet
			response := rc.messageBuilder.RejectOpenChannel(opm.GetChannelIdentifier(), "GenericError")
			rc.traceLog(fmt.Sprintf("sending reject open channel for %v", opm.GetChannelIdentifier()))
			rc.SendRicochetPacket(rc.Conn, 0, response)

		}
	} else if res.GetChannelResult() != nil {
		cr := res.GetChannelResult()
		id := cr.GetChannelIdentifier()

		channel, found := rc.channelManager.GetChannel(id)

		if !found {
			rc.traceLog(fmt.Sprintf("channel result recived for unknown channel: %v", channel.Type, id))
			return
		}

		if cr.GetOpened() {
			rc.traceLog(fmt.Sprintf("channel of type %v opened on %v", channel.Type, id))
			channel.Handler.OpenOutboundResult(nil, cr)
		} else {
			rc.traceLog(fmt.Sprintf("channel of type %v rejected on %v", channel.Type, id))
			channel.Handler.OpenOutboundResult(errors.New(""), cr)
		}

	} else if res.GetKeepAlive() != nil {
		// XXX Though not currently part of the protocol
		// We should likely put these calls behind
		// authentication.
		rc.traceLog("received keep alive packet")
		if res.GetKeepAlive().GetResponseRequested() {
			messageBuilder := new(utils.MessageBuilder)
			raw := messageBuilder.KeepAlive(true)
			rc.traceLog("sending keep alive response")
			rc.SendRicochetPacket(rc.Conn, 0, raw)
		}
	} else if res.GetEnableFeatures() != nil {
		rc.traceLog("received features enabled packet")
		messageBuilder := new(utils.MessageBuilder)
		raw := messageBuilder.FeaturesEnabled([]string{})
		rc.traceLog("sending featured enabled empty response")
		rc.SendRicochetPacket(rc.Conn, 0, raw)
	} else if res.GetFeaturesEnabled() != nil {
		// TODO We should never send out an enabled features
		// request.
		rc.traceLog("sending unsolicited features enabled response")
	}
}

func (rc *Connection) traceLog(message string) {
	if rc.trace {
		log.Printf(message)
	}
}

// Break causes Process() to return, but does not close the underlying connection
// Break returns an error if it would not be valid to call Process() again for
// the connection now. Currently, the only such error is ConnectionClosedError.
func (rc *Connection) Break() error {
	// See Do() for an explanation of the concurrency here; it's complicated.
	// The summary is that this mutex prevents races on connection close that
	// could lead to deadlocks in Block().
	rc.processBlockMutex.Lock()
	defer rc.processBlockMutex.Unlock()
	if rc.closed {
		rc.traceLog("ignoring break because connection is already closed")
		return utils.ConnectionClosedError
	}
	rc.traceLog("breaking out of process loop")
	rc.breakChannel <- true
	return <-rc.breakResultChannel // Wait for Process to End
}

// Channel is a convienciance method for returning a given channel to the caller
// of Process() - TODO - this is kind of ugly.
func (rc *Connection) Channel(ctype string, way channels.Direction) *channels.Channel {
	return rc.channelManager.Channel(ctype, way)
}
