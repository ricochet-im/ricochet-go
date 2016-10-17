package main

import (
	"errors"
	"fmt"
	"github.com/ricochet-im/ricochet-go/rpc"
)

type ContactList struct {
	Contacts map[int32]*Contact
}

func NewContactList() *ContactList {
	return &ContactList{
		Contacts: make(map[int32]*Contact),
	}
}

func (cl *ContactList) Populate(data *ricochet.Contact) error {
	if cl.Contacts[data.Id] != nil {
		return fmt.Errorf("Duplicate contact ID %d in populate", data.Id)
	}

	cl.Contacts[data.Id] = initContact(data)
	return nil
}

func (cl *ContactList) Added(data *ricochet.Contact) (*Contact, error) {
	if cl.Contacts[data.Id] != nil {
		return nil, fmt.Errorf("Duplicate contact ID %d in add", data.Id)
	}

	contact := initContact(data)
	cl.Contacts[data.Id] = contact
	return contact, nil
}

func (cl *ContactList) Deleted(data *ricochet.Contact) (*Contact, error) {
	contact := cl.Contacts[data.Id]
	if contact == nil {
		return nil, fmt.Errorf("Contact ID %d does not exist in delete", data.Id)
	}

	if contact.Data.Address != data.Address {
		return nil, fmt.Errorf("Contact ID %d does not match address in delete (expected %s, received %s)", data.Id, contact.Data.Address, data.Address)
	}

	contact.Deleted()
	delete(cl.Contacts, data.Id)
	return contact, nil
}

func (cl *ContactList) ById(id int32) *Contact {
	return cl.Contacts[id]
}

func (cl *ContactList) ByIdAndAddress(id int32, address string) *Contact {
	contact := cl.Contacts[id]
	if contact != nil && contact.Data.Address == address {
		return contact
	}
	return nil
}

type Contact struct {
	Data *ricochet.Contact
}

func initContact(data *ricochet.Contact) *Contact {
	return &Contact{
		Data: data,
	}
}

func (c *Contact) Updated(newData *ricochet.Contact) error {
	if newData.Id != c.Data.Id || newData.Address != c.Data.Address {
		return errors.New("Contact ID and address are immutable")
	}

	c.Data = newData
	return nil
}

func (c *Contact) Deleted() {
	c.Data = &ricochet.Contact{
		Id:      c.Data.Id,
		Address: c.Data.Address,
	}
}
