package main

import (
	"github.com/s-rah/go-ricochet/application"
	"github.com/s-rah/go-ricochet/utils"
	"log"
	"time"
)

func main() {
	echobot := new(application.RicochetApplication)
	pk, err := utils.LoadPrivateKeyFromFile("./testing/private_key")

	if err != nil {
		log.Fatalf("error reading private key file: %v", err)
	}

	l, err := application.SetupOnion("127.0.0.1:9051", "", pk, 9878)

	if err != nil {
		log.Fatalf("error setting up onion service: %v", err)
	}

	echobot.Init(pk, new(application.AcceptAllContactManager))
	echobot.OnChatMessage(func(rai *application.RicochetApplicationInstance, id uint32, timestamp time.Time, message string) {
		log.Printf("message from %v - %v", rai.RemoteHostname, message)
		rai.SendChatMessage(message)
	})
	log.Printf("echobot listening on %s", l.Addr().String())
	echobot.Run(l)
}
