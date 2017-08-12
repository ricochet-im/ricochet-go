package main

import (
	"github.com/s-rah/go-ricochet"
	"github.com/s-rah/go-ricochet/channels"
	"github.com/s-rah/go-ricochet/connection"
	"github.com/s-rah/go-ricochet/utils"
	"log"
	"time"
)

// EchoBotService is an example service which simply echoes back what a client
// sends it.
type RicochetEchoBot struct {
	connection.AutoConnectionHandler
	messages chan string
}

func (echobot *RicochetEchoBot) ContactRequest(name string, message string) string {
	return "Pending"
}

func (echobot *RicochetEchoBot) ContactRequestRejected() {
}
func (echobot *RicochetEchoBot) ContactRequestAccepted() {
}
func (echobot *RicochetEchoBot) ContactRequestError() {
}

func (echobot *RicochetEchoBot) ChatMessage(messageID uint32, when time.Time, message string) bool {
	echobot.messages <- message
	return true
}

func (echobot *RicochetEchoBot) ChatMessageAck(messageID uint32) {

}

func (echobot *RicochetEchoBot) Connect(privateKeyFile string, hostname string) {

	privateKey, _ := utils.LoadPrivateKeyFromFile(privateKeyFile)
	echobot.messages = make(chan string)

	echobot.Init()
	echobot.RegisterChannelHandler("im.ricochet.contact.request", func() channels.Handler {
		contact := new(channels.ContactRequestChannel)
		contact.Handler = echobot
		return contact
	})
	echobot.RegisterChannelHandler("im.ricochet.chat", func() channels.Handler {
		chat := new(channels.ChatChannel)
		chat.Handler = echobot
		return chat
	})

	rc, _ := goricochet.Open(hostname)
	known, err := connection.HandleOutboundConnection(rc).ProcessAuthAsClient(privateKey)
	if err == nil {

		go rc.Process(echobot)

		if !known {
			err := rc.Do(func() error {
				_, err := rc.RequestOpenChannel("im.ricochet.contact.request",
					&channels.ContactRequestChannel{
						Handler: echobot,
						Name:    "EchoBot",
						Message: "I LIVE 😈😈!!!!",
					})
				return err
			})
			if err != nil {
				log.Printf("could not contact %s", err)
			}
		}

		rc.Do(func() error {
			_, err := rc.RequestOpenChannel("im.ricochet.chat", &channels.ChatChannel{Handler: echobot})
			return err
		})
		for {
			message := <-echobot.messages
			log.Printf("Received Message: %s", message)
			rc.Do(func() error {
				log.Printf("Finding Chat Channel")
				channel := rc.Channel("im.ricochet.chat", channels.Outbound)
				if channel != nil {
					log.Printf("Found Chat Channel")
					chatchannel, ok := channel.Handler.(*channels.ChatChannel)
					if ok {
						chatchannel.SendMessage(message)
					}
				} else {
					log.Printf("Could not find chat channel")
				}
				return nil
			})
		}
	}
}

func main() {
	echoBot := new(RicochetEchoBot)
	echoBot.Connect("private_key", "oqf7z4ot6kuejgam")
}
