package main

import (
	"github.com/s-rah/go-ricochet"
	"log"
)

// EchoBotService is an example service which simply echoes back what a client
// sends it.
type EchoBotService struct {
	goricochet.StandardRicochetService
}

func (ebs *EchoBotService) OnNewConnection(oc *goricochet.OpenConnection) {
	ebs.StandardRicochetService.OnNewConnection(oc)
	go oc.Process(&EchoBotConnection{})
}

type EchoBotConnection struct {
	goricochet.StandardRicochetConnection
}

// IsKnownContact is configured to always accept Contact Requests
func (ebc *EchoBotConnection) IsKnownContact(hostname string) bool {
	return true
}

// OnContactRequest - we always accept new contact request.
func (ebc *EchoBotConnection) OnContactRequest(channelID int32, nick string, message string) {
	ebc.StandardRicochetConnection.OnContactRequest(channelID, nick, message)
	ebc.Conn.AckContactRequestOnResponse(channelID, "Accepted")
	ebc.Conn.CloseChannel(channelID)
}

// OnChatMessage we acknowledge the message, grab the message content and send it back - opening
// a new channel if necessary.
func (ebc *EchoBotConnection) OnChatMessage(channelID int32, messageID int32, message string) {
	log.Printf("Received Message from %s: %s", ebc.Conn.OtherHostname, message)
	ebc.Conn.AckChatMessage(channelID, messageID)
	if ebc.Conn.GetChannelType(6) == "none" {
		ebc.Conn.OpenChatChannel(6)
	}
	ebc.Conn.SendMessage(6, message)
}

func main() {
	ricochetService := new(EchoBotService)
	ricochetService.Init("./private_key")
	ricochetService.Listen(ricochetService, 12345)
}
