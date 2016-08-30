package core

import (
	"errors"
	"fmt"
	"strconv"
)

type ContactList struct {
	contacts         map[int]*Contact
	outboundRequests map[int]*OutboundContactRequest
	inboundRequests  map[int]*InboundContactRequest
}

func LoadContactList(core Ricochet) (*ContactList, error) {
	list := &ContactList{}
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

func (this *ContactList) Contacts() []*Contact {
	re := make([]*Contact, 0, len(this.contacts))
	for _, contact := range this.contacts {
		re = append(re, contact)
	}
	return re
}

func (this *ContactList) OutboundRequests() []*OutboundContactRequest {
	re := make([]*OutboundContactRequest, 0, len(this.outboundRequests))
	for _, request := range this.outboundRequests {
		re = append(re, request)
	}
	return re
}

func (this *ContactList) InboundRequests() []*InboundContactRequest {
	re := make([]*InboundContactRequest, 0, len(this.inboundRequests))
	for _, request := range this.inboundRequests {
		re = append(re, request)
	}
	return re
}

func (this *ContactList) ContactById(id int) *Contact {
	return this.contacts[id]
}

func (this *ContactList) ContactByAddress(address string) *Contact {
	for _, contact := range this.contacts {
		if contact.Address() == address {
			return contact
		}
	}
	return nil
}

func (contacts *ContactList) AddContact(address string, name string) (*Contact, error) {
	return nil, errors.New("Not implemented")
}

func (contacts *ContactList) RemoveContactById(id int) error {
	return errors.New("Not implemented")
}
