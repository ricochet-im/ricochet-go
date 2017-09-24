package main

import (
	"errors"
	"fmt"
	"github.com/ricochet-im/ricochet-go/rpc"
)

type ContactList struct {
	Client   *Client
	Contacts map[string]*Contact
}

func NewContactList(client *Client) *ContactList {
	return &ContactList{
		Client:   client,
		Contacts: make(map[string]*Contact),
	}
}

func (cl *ContactList) Populate(data *ricochet.Contact) error {
	if cl.Contacts[data.Address] != nil {
		return fmt.Errorf("Duplicate contact %s in populate", data.Address)
	}

	cl.Contacts[data.Address] = initContact(cl.Client, data)
	return nil
}

func (cl *ContactList) Added(data *ricochet.Contact) (*Contact, error) {
	if cl.Contacts[data.Address] != nil {
		return nil, fmt.Errorf("Duplicate contact %s in add", data.Address)
	}

	contact := initContact(cl.Client, data)
	cl.Contacts[data.Address] = contact
	return contact, nil
}

func (cl *ContactList) Deleted(data *ricochet.Contact) (*Contact, error) {
	contact := cl.Contacts[data.Address]
	if contact == nil {
		return nil, fmt.Errorf("Contact %s does not exist in delete", data.Address)
	}

	contact.Deleted()
	delete(cl.Contacts, data.Address)
	return contact, nil
}

func (cl *ContactList) ByAddress(address string) *Contact {
	return cl.Contacts[address]
}

type Contact struct {
	Data         *ricochet.Contact
	Conversation *Conversation
}

func initContact(client *Client, data *ricochet.Contact) *Contact {
	c := &Contact{
		Data: data,
	}
	c.Conversation = &Conversation{
		Client:  client,
		Contact: c,
	}
	return c
}

func (c *Contact) Updated(newData *ricochet.Contact) error {
	if newData.Address != c.Data.Address {
		return errors.New("Contact address is immutable")
	}

	c.Data = newData
	return nil
}

func (c *Contact) Deleted() {
	c.Data = &ricochet.Contact{
		Address: c.Data.Address,
	}
}
