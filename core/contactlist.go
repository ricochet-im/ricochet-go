package core

import (
	"errors"
)

type ContactList struct {
	contacts         map[int]*Contact
	outboundRequests map[int]*OutboundContactRequest
	inboundRequests  map[int]*InboundContactRequest
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
		if contact.Address == address {
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
