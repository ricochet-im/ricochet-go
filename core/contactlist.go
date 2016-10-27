package core

import (
	"errors"
	"fmt"
	"github.com/ricochet-im/ricochet-go/core/utils"
	"github.com/ricochet-im/ricochet-go/rpc"
	"strconv"
	"sync"
	"time"
)

type ContactList struct {
	core *Ricochet

	mutex  sync.RWMutex
	events *utils.Publisher

	contacts map[int]*Contact
}

func LoadContactList(core *Ricochet) (*ContactList, error) {
	list := &ContactList{
		core:   core,
		events: utils.CreatePublisher(),
	}

	config := core.Config.OpenRead()
	defer config.Close()

	list.contacts = make(map[int]*Contact, len(config.Contacts))
	for idStr, data := range config.Contacts {
		id, err := strconv.Atoi(idStr)
		if err != nil {
			return nil, fmt.Errorf("Invalid contact id '%s'", idStr)
		}
		if _, exists := list.contacts[id]; exists {
			return nil, fmt.Errorf("Duplicate contact id '%d'", id)
		}

		contact, err := ContactFromConfig(core, id, data, list.events)
		if err != nil {
			return nil, err
		}
		list.contacts[id] = contact
	}

	// XXX Requests aren't implemented
	return list, nil
}

func (this *ContactList) EventMonitor() utils.Subscribable {
	return this.events
}

func (this *ContactList) Contacts() []*Contact {
	this.mutex.RLock()
	defer this.mutex.RUnlock()
	re := make([]*Contact, 0, len(this.contacts))
	for _, contact := range this.contacts {
		re = append(re, contact)
	}
	return re
}

func (this *ContactList) ContactById(id int) *Contact {
	this.mutex.RLock()
	defer this.mutex.RUnlock()
	return this.contacts[id]
}

func (this *ContactList) ContactByAddress(address string) *Contact {
	this.mutex.RLock()
	defer this.mutex.RUnlock()
	for _, contact := range this.contacts {
		if contact.Address() == address {
			return contact
		}
	}
	return nil
}

func (this *ContactList) AddContactRequest(address, name, fromName, text string) (*Contact, error) {
	this.mutex.Lock()
	defer this.mutex.Unlock()

	// XXX check that address is valid before relying on format below
	// XXX validity checks on name/text also useful

	for _, contact := range this.contacts {
		if contact.Address() == address {
			return nil, errors.New("Contact already exists with this address")
		}
		if contact.Nickname() == name {
			return nil, errors.New("Contact already exists with this nickname")
		}
	}

	// XXX check inbound requests

	// Write new contact into config
	config := this.core.Config.OpenWrite()

	maxContactId := 0
	for idstr, _ := range config.Contacts {
		if id, err := strconv.Atoi(idstr); err == nil {
			if maxContactId < id {
				maxContactId = id
			}
		}
	}

	contactId := maxContactId + 1
	configContact := ConfigContact{
		Hostname:    address[9:] + ".onion",
		Nickname:    name,
		WhenCreated: time.Now().Format(time.RFC3339),
		Request: ConfigContactRequest{
			Pending:    true,
			MyNickname: fromName,
			Message:    text,
		},
	}

	config.Contacts[strconv.Itoa(contactId)] = configContact
	if err := config.Save(); err != nil {
		return nil, err
	}

	// Create Contact
	// XXX This starts connection immediately, which could cause contact update
	// events before the add event in an unlikely race case
	contact, err := ContactFromConfig(this.core, contactId, configContact, this.events)
	if err != nil {
		return nil, err
	}
	this.contacts[contactId] = contact

	event := ricochet.ContactEvent{
		Type: ricochet.ContactEvent_ADD,
		Subject: &ricochet.ContactEvent_Contact{
			Contact: contact.Data(),
		},
	}
	this.events.Publish(event)

	return contact, nil
}

func (this *ContactList) RemoveContact(contact *Contact) error {
	this.mutex.Lock()
	defer this.mutex.Unlock()

	if this.contacts[contact.Id()] != contact {
		return errors.New("Not in contact list")
	}

	// XXX Not persisting in config
	// XXX This will have to do some things to the contact itself
	// eventually too, such as killing connections and other resources.
	delete(this.contacts, contact.Id())

	event := ricochet.ContactEvent{
		Type: ricochet.ContactEvent_DELETE,
		Subject: &ricochet.ContactEvent_Contact{
			Contact: &ricochet.Contact{
				Id:      int32(contact.Id()),
				Address: contact.Address(),
			},
		},
	}
	this.events.Publish(event)

	return nil
}
