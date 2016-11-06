package core

import (
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"github.com/ricochet-im/ricochet-go/core/utils"
	protocol "github.com/s-rah/go-ricochet"
	"github.com/yawning/bulb/utils/pkcs1"
	"log"
	"sync"
)

// Identity represents the local user, including their contact address,
// and contains the contacts list.
type Identity struct {
	core *Ricochet

	mutex sync.Mutex

	address     string
	privateKey  *rsa.PrivateKey
	contactList *ContactList

	ConversationStream *utils.Publisher
}

func CreateIdentity(core *Ricochet) (*Identity, error) {
	me := &Identity{
		core:               core,
		ConversationStream: utils.CreatePublisher(),
	}

	if err := me.loadIdentity(); err != nil {
		log.Printf("Failed loading identity: %v", err)
		return nil, err
	}

	contactList, err := LoadContactList(core)
	if err != nil {
		log.Printf("Failed loading contact list: %v", err)
		return nil, err
	}
	me.contactList = contactList

	contactList.StartConnections()
	go me.publishService(me.privateKey)
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
		me.address, err = AddressFromKey(&me.privateKey.PublicKey)
		if err != nil {
			return err
		}

		log.Printf("Loaded identity %s", me.address)
	} else {
		log.Printf("Initializing new identity")
	}

	return nil
}

func (me *Identity) setPrivateKey(key *rsa.PrivateKey) error {
	me.mutex.Lock()
	defer me.mutex.Unlock()

	if me.privateKey != nil || me.address != "" {
		return errors.New("Cannot change private key on identity")
	}

	// Save key to config
	keyData, err := pkcs1.EncodePrivateKeyDER(key)
	if err != nil {
		return err
	}
	config := me.core.Config.OpenWrite()
	config.Identity.ServiceKey = base64.StdEncoding.EncodeToString(keyData)
	config.Save()

	// Update Identity
	me.address, err = AddressFromKey(&key.PublicKey)
	if err != nil {
		return err
	}
	me.privateKey = key

	log.Printf("Created new identity %s", me.address)
	return nil
}

type identityService struct {
	Identity   *Identity
	MyHostname string
}

func (is *identityService) OnNewConnection(oc *protocol.OpenConnection) {
	log.Printf("Inbound connection accepted")
	oc.MyHostname = is.MyHostname
	// XXX Should have pre-auth handling, timeouts
	identity := is.Identity
	handler := &ProtocolConnection{
		Conn: oc,
		GetContactByHostname: func(hostname string) *Contact {
			address, ok := AddressFromPlainHost(hostname)
			if !ok {
				return nil
			}
			return identity.ContactList().ContactByAddress(address)
		},
	}
	go oc.Process(handler)
}

func (is *identityService) OnFailedConnection(err error) {
	log.Printf("Inbound connection failed: %v", err)
}

// BUG(special): No error handling for failures under publishService
func (me *Identity) publishService(key *rsa.PrivateKey) {
	// This call will block until a control connection is available and the
	// ADD_ONION command has returned. After creating the listener, it will
	// be automatically re-published if the control connection is lost and
	// later reconnected.
	service, listener, err := me.core.Network.NewOnionListener(9878, key)
	if err != nil {
		log.Printf("Identity listener failed: %v", err)
		// XXX handle
		return
	}

	if key == nil {
		if service.PrivateKey == nil {
			log.Printf("Setting private key failed: no key returned")
			// XXX handle
			return
		}

		err := me.setPrivateKey(service.PrivateKey.(*rsa.PrivateKey))
		if err != nil {
			log.Printf("Setting private key failed: %v", err)
			// XXX handle
			return
		}
	}

	log.Printf("Identity service published, accepting connections")
	is := &identityService{
		Identity:   me,
		MyHostname: me.Address()[9:],
	}

	err = protocol.Serve(listener, is)
	if err != nil {
		log.Printf("Identity listener failed: %v", err)
		// XXX handle
		return
	}
}

func (me *Identity) Address() string {
	return me.address
}

func (me *Identity) ContactList() *ContactList {
	return me.contactList
}

func (me *Identity) PrivateKey() rsa.PrivateKey {
	return *me.privateKey
}
