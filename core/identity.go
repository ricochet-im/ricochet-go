package core

import (
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"github.com/yawning/bulb/utils/pkcs1"
	"log"
)

type Identity struct {
	core *Ricochet

	address    string
	privateKey *rsa.PrivateKey

	contactList *ContactList
}

func CreateIdentity(core *Ricochet) (*Identity, error) {
	me := &Identity{
		core: core,
	}

	err := me.loadIdentity()
	if err != nil {
		log.Printf("Loading identity failed: %v", err)
		return nil, err
	}

	contactList, err := LoadContactList(core)
	if err != nil {
		log.Printf("Loading contact list failed: %v", err)
		return nil, err
	}
	me.contactList = contactList

	return me, nil
}

func (me *Identity) loadIdentity() error {
	config := me.core.Config.OpenRead()
	defer config.Close()

	if config.Identity.ServiceKey != "" {
		keyData, err := base64.StdEncoding.DecodeString(config.Identity.ServiceKey)
		if err != nil {
			return err
		}

		me.privateKey, _, err = pkcs1.DecodePrivateKeyDER(keyData)
		if err != nil {
			return err
		}

		me.address, err = pkcs1.OnionAddr(&me.privateKey.PublicKey)
		if err != nil {
			return err
		} else if me.address == "" {
			return errors.New("Invalid onion address")
		}
		me.address = "ricochet:" + me.address
	}

	return nil
}

func (me *Identity) Address() string {
	return me.address
}

func (me *Identity) ContactList() *ContactList {
	return me.contactList
}
