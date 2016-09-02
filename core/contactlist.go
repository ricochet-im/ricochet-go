package core

import (
	"errors"
	"fmt"
	"github.com/special/notricochet/core/utils"
	"github.com/special/notricochet/rpc"
	"strconv"
	"sync"
)

type ContactList struct {
	mutex  sync.RWMutex
	events *utils.Publisher

	contacts         map[int]*Contact
	outboundRequests map[int]*OutboundContactRequest
	inboundRequests  map[int]*InboundContactRequest
}

func LoadContactList(core Ricochet) (*ContactList, error) {
	list := &ContactList{
		events: utils.CreatePublisher(),
	}

	config := core.Config().OpenRead()
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

		contact, err := ContactFromConfig(id, data)
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

func (this *ContactList) OutboundRequests() []*OutboundContactRequest {
	this.mutex.RLock()
	defer this.mutex.RUnlock()
	re := make([]*OutboundContactRequest, 0, len(this.outboundRequests))
	for _, request := range this.outboundRequests {
		re = append(re, request)
	}
	return re
}

func (this *ContactList) InboundRequests() []*InboundContactRequest {
	this.mutex.RLock()
	defer this.mutex.RUnlock()
	re := make([]*InboundContactRequest, 0, len(this.inboundRequests))
	for _, request := range this.inboundRequests {
		re = append(re, request)
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

func (this *ContactList) AddContact(address string, name string) (*Contact, error) {
	return nil, errors.New("Not implemented")
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
