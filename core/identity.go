package core

import (
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"github.com/yawning/bulb/utils/pkcs1"
	"log"
)

type Identity struct {
	address    string
	privateKey *rsa.PrivateKey

	contactList *ContactList
}

func CreateIdentity(core Ricochet) (*Identity, error) {
	me := &Identity{}

	config := core.Config().OpenRead()
	defer config.Close()
	if config.Identity.ServiceKey != "" {
		keyData, err := base64.StdEncoding.DecodeString(config.Identity.ServiceKey)
		if err != nil {
			log.Printf("Decoding identity key failed: %v", err)
			return nil, err
		}

		me.privateKey, _, err = pkcs1.DecodePrivateKeyDER(keyData)
		if err != nil {
			log.Printf("Decoding identity key failed: %v", err)
			return nil, err
		}

		me.address, err = pkcs1.OnionAddr(&me.privateKey.PublicKey)
		if err != nil && me.address == "" {
			err = errors.New("Cannot calculate onion address")
		}
		if err != nil {
			log.Printf("Decoding identify key failed: %v", err)
			return nil, err
		}
		me.address = "ricochet:" + me.address
	}

	return me, nil
}

func (me *Identity) Address() string {
	return me.address
}

func (me *Identity) ContactList() *ContactList {
	return me.contactList
}
