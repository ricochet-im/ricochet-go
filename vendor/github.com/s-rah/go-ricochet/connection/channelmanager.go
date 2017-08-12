package connection

import (
	"github.com/s-rah/go-ricochet/channels"
	"github.com/s-rah/go-ricochet/utils"
)

// ChannelManager encapsulates the logic for server and client side assignment
// and removal of channels.
type ChannelManager struct {
	channels        map[int32]*channels.Channel
	nextFreeChannel int32
	isClient        bool
}

// NewClientChannelManager construsts a new channel manager enforcing behaviour
// of a ricochet client
func NewClientChannelManager() *ChannelManager {
	channelManager := new(ChannelManager)
	channelManager.channels = make(map[int32]*channels.Channel)
	channelManager.nextFreeChannel = 1
	channelManager.isClient = true
	return channelManager
}

// NewServerChannelManager construsts a new channel manager enforcing behaviour
// from a ricochet server
func NewServerChannelManager() *ChannelManager {
	channelManager := new(ChannelManager)
	channelManager.channels = make(map[int32]*channels.Channel)
	channelManager.nextFreeChannel = 2
	channelManager.isClient = false
	return channelManager
}

// OpenChannelRequest  constructs a channel type ready for processing given a request
// from the client.
func (cm *ChannelManager) OpenChannelRequest(chandler channels.Handler) (*channels.Channel, error) {
	// Some channels only allow us to open one of them per connection
	if chandler.Singleton() && cm.Channel(chandler.Type(), channels.Outbound) != nil {
		return nil, utils.AttemptToOpenMoreThanOneSingletonChannelError
	}

	channel := new(channels.Channel)
	channel.ID = cm.nextFreeChannel
	cm.nextFreeChannel += 2
	channel.Type = chandler.Type()
	channel.Handler = chandler
	channel.Pending = true
	channel.Direction = channels.Outbound
	cm.channels[channel.ID] = channel
	return channel, nil
}

// OpenChannelRequestFromPeer constructs a channel type ready for processing given a request
// from the remote peer.
func (cm *ChannelManager) OpenChannelRequestFromPeer(channelID int32, chandler channels.Handler) (*channels.Channel, error) {
	if cm.isClient && (channelID%2) != 0 {
		// Server is trying to open odd numbered channels
		return nil, utils.ServerAttemptedToOpenEvenNumberedChannelError
	} else if !cm.isClient && (channelID%2) == 0 {
		// Server is trying to open odd numbered channels
		return nil, utils.ClientAttemptedToOpenOddNumberedChannelError
	}

	_, exists := cm.channels[channelID]
	if exists {
		return nil, utils.ChannelIDIsAlreadyInUseError
	}

	// Some channels only allow us to open one of them per connection
	if chandler.Singleton() && cm.Channel(chandler.Type(), channels.Inbound) != nil {
		return nil, utils.AttemptToOpenMoreThanOneSingletonChannelError
	}

	channel := new(channels.Channel)
	channel.ID = channelID
	channel.Type = chandler.Type()
	channel.Handler = chandler

	channel.Pending = true
	channel.Direction = channels.Inbound
	cm.channels[channelID] = channel
	return channel, nil
}

// Channel finds an open or pending `type` channel in the direction `way` (Inbound
// or Outbound), and returns the associated state. Returns nil if no matching channel
// exists or if multiple matching channels exist.
func (cm *ChannelManager) Channel(ctype string, way channels.Direction) *channels.Channel {
	var foundChannel *channels.Channel
	for _, channel := range cm.channels {
		if channel.Handler.Type() == ctype && channel.Direction == way {
			if foundChannel == nil {
				foundChannel = channel
			} else {
				// we have found multiple channels.
				return nil
			}
		}
	}
	return foundChannel
}

// GetChannel finds and returns a given channel if it is found
func (cm *ChannelManager) GetChannel(channelID int32) (*channels.Channel, bool) {
	channel, found := cm.channels[channelID]
	return channel, found
}

// RemoveChannel removes a given channel id.
func (cm *ChannelManager) RemoveChannel(channelID int32) {
	delete(cm.channels, channelID)
}
