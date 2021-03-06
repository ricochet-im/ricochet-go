package core

import (
	"errors"
	"fmt"
	"github.com/ricochet-im/ricochet-go/core/utils"
	"github.com/ricochet-im/ricochet-go/rpc"
	"sync"
	"time"
)

type ContactList struct {
	core *Ricochet

	mutex  sync.RWMutex
	events *utils.Publisher

	contacts        map[string]*Contact
	inboundRequests map[string]*InboundContactRequest
}

func LoadContactList(core *Ricochet) (*ContactList, error) {
	list := &ContactList{
		core:            core,
		events:          utils.CreatePublisher(),
		inboundRequests: make(map[string]*InboundContactRequest),
	}

	config := core.Config.Read()
	list.contacts = make(map[string]*Contact, len(config.Contacts))
	for addr, data := range config.Contacts {
		if _, exists := list.contacts[addr]; exists {
			return nil, fmt.Errorf("Duplicate contact %s", addr)
		}
		if addr != data.Address {
			return nil, fmt.Errorf("Contact address/key do not match ('%s' and '%s')", addr, data.Address)
		}

		contact, err := ContactFromConfig(core, data, list.events)
		if err != nil {
			return nil, err
		}
		list.contacts[addr] = contact
	}

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

func (cl *ContactList) ContactByAddress(address string) *Contact {
	cl.mutex.RLock()
	defer cl.mutex.RUnlock()
	return cl.contacts[address]
}

func (cl *ContactList) InboundRequestByAddress(address string) *InboundContactRequest {
	cl.mutex.RLock()
	defer cl.mutex.RUnlock()
	return cl.inboundRequests[address]
}

// AddNewContact adds a new contact to the persistent contact list, broadcasts a
// contact add RPC event, and returns a newly constructed Contact. AddNewContact
// does not create contact requests or trigger any other protocol behavior.
//
// Generally, you will use AddContactRequest (for outbound requests) and
// AddOrUpdateInboundContactRequest plus InboundContactRequest.Accept() instead of
// using this function directly.
func (this *ContactList) AddNewContact(data *ricochet.Contact) (*Contact, error) {
	this.mutex.Lock()
	defer this.mutex.Unlock()

	if this.contacts[data.Address] != nil {
		return nil, errors.New("Contact already exists with this address")
	}
	for _, contact := range this.contacts {
		if contact.Nickname() == data.Nickname {
			return nil, errors.New("Contact already exists with this nickname")
		}
	}

	// XXX check inbound requests (but this can be called for an inbound req too)

	// Write new contact into config
	config := this.core.Config.Lock()
	if config.Contacts == nil {
		config.Contacts = make(map[string]*ricochet.Contact)
	}
	config.Contacts[data.Address] = data
	this.core.Config.Unlock()

	// Create Contact
	contact, err := ContactFromConfig(this.core, data, this.events)
	if err != nil {
		return nil, err
	}
	this.contacts[data.Address] = contact

	event := ricochet.ContactEvent{
		Type: ricochet.ContactEvent_ADD,
		Subject: &ricochet.ContactEvent_Contact{
			Contact: contact.Data(),
		},
	}
	this.events.Publish(event)

	// XXX Should this be here? Is it ok for inbound where we might pass conn over momentarily?
	contact.StartConnection()
	return contact, nil
}

// AddContactRequest creates a new outbound contact request with the given parameters,
// adds it to the contact list, and returns the newly constructed Contact.
//
// If an inbound request already exists for this address, that request will be automatically
// accepted, and the returned contact will already be fully established.
func (cl *ContactList) AddContactRequest(address, name, fromName, text string) (*Contact, error) {
	if !IsAddressValid(address) {
		return nil, errors.New("Invalid ricochet address")
	}
	if !IsNicknameAcceptable(name) {
		return nil, errors.New("Invalid nickname")
	}
	if len(fromName) > 0 && !IsNicknameAcceptable(fromName) {
		return nil, errors.New("Invalid 'from' nickname")
	}
	if len(text) > 0 && !IsMessageAcceptable(text) {
		return nil, errors.New("Invalid message")
	}

	data := &ricochet.Contact{
		Address:     address,
		Nickname:    name,
		WhenCreated: time.Now().Format(time.RFC3339),
		Request: &ricochet.ContactRequest{
			Direction:    ricochet.ContactRequest_OUTBOUND,
			Address:      address,
			Nickname:     name,
			FromNickname: fromName,
			Text:         text,
			WhenCreated:  time.Now().Format(time.RFC3339),
		},
	}
	contact, err := cl.AddNewContact(data)
	if err != nil {
		return nil, err
	}

	if inboundRequest := cl.InboundRequestByAddress(address); inboundRequest != nil {
		contact.UpdateContactRequest("Accepted")
		inboundRequest.AcceptWithContact(contact)
	}

	return contact, nil
}

func (this *ContactList) RemoveContact(contact *Contact) error {
	this.mutex.Lock()
	defer this.mutex.Unlock()

	address := contact.Address()
	if this.contacts[address] != contact {
		return errors.New("Not in contact list")
	}

	// XXX How do we safely make sure that the contact has stopped everything, and that
	// nobody is going to block on it or keep referencing it..? This is insufficient, it
	// leaves a goroutine up among other things.
	contact.StopConnection()

	config := this.core.Config.Lock()
	delete(config.Contacts, address)
	this.core.Config.Unlock()

	delete(this.contacts, address)

	event := ricochet.ContactEvent{
		Type: ricochet.ContactEvent_DELETE,
		Subject: &ricochet.ContactEvent_Contact{
			Contact: &ricochet.Contact{
				Address: address,
			},
		},
	}
	this.events.Publish(event)

	return nil
}

// AddOrUpdateInboundContactRequest creates or modifies an inbound contact request for
// the hostname, with an optional nickname suggestion and introduction message
//
// The nickname, message, and address must be validated before calling this function.
//
// This function may change the state of the request, and the caller is responsible for
// sending a reply message and closing the channel or connection as appropriate. If the
// request is still active when the state changes spontaneously later (e.g. it's accepted
// by the user), replies will be sent by InboundContactRequest.
//
// This function may return either an InboundContactRequest (which may be pending or already
// rejected), an existing contact (which should be treated as accepting the request), or
// neither, which is considered a rejection.
func (cl *ContactList) AddOrUpdateInboundContactRequest(address, nickname, message string) (*InboundContactRequest, *Contact) {
	cl.mutex.Lock()
	defer cl.mutex.Unlock()

	// Look up existing request
	if request := cl.inboundRequests[address]; request != nil {
		// Errors in Update will change the state of the request, which the caller sends as a reply
		request.Update(nickname, message)
		return request, nil
	}

	// Check for existing contacts or outbound contact requests
	for _, contact := range cl.contacts {
		if contact.Address() == address {
			if contact.IsRequest() {
				contact.UpdateContactRequest("Accepted")
			}
			return nil, contact
		}
	}

	// Create new request
	request := CreateInboundContactRequest(cl.core, address, nickname, message)
	request.StatusChanged = cl.inboundRequestChanged
	cl.inboundRequests[address] = request
	// XXX update config
	requestData := request.Data()
	event := ricochet.ContactEvent{
		Type: ricochet.ContactEvent_ADD,
		Subject: &ricochet.ContactEvent_Request{
			Request: &requestData,
		},
	}
	cl.events.Publish(event)
	return request, nil
}

// RemoveInboundContactRequest removes the record of an inbound contact request,
// without taking any other actions. Generally you will want to act on a request with
// Accept() or Reject(), rather than call this function directly, but it is valid to
// call on any inbound request regardless.
func (cl *ContactList) RemoveInboundContactRequest(request *InboundContactRequest) error {
	cl.mutex.Lock()
	defer cl.mutex.Unlock()
	requestData := request.Data()

	if cl.inboundRequests[requestData.Address] != request {
		return errors.New("Request is not in contact list")
	}

	// Close connection asynchronously to avoid potential deadlocking
	go request.CloseConnection()
	// XXX Remove from config

	delete(cl.inboundRequests, requestData.Address)

	event := ricochet.ContactEvent{
		Type: ricochet.ContactEvent_DELETE,
		Subject: &ricochet.ContactEvent_Request{
			Request: &requestData,
		},
	}
	cl.events.Publish(event)
	return nil
}

// inboundRequestChanged is called by the StatusChanged callback of InboundContactRequest
// after the request has been modified, such as through Update() or Reject(). Accepted
// requests do not pass through this function, because they're immediately removed.
func (cl *ContactList) inboundRequestChanged(request *InboundContactRequest) {
	// XXX update config
	requestData := request.Data()
	event := ricochet.ContactEvent{
		Type: ricochet.ContactEvent_UPDATE,
		Subject: &ricochet.ContactEvent_Request{
			Request: &requestData,
		},
	}
	cl.events.Publish(event)
}

func (this *ContactList) StartConnections() {
	for _, contact := range this.Contacts() {
		contact.StartConnection()
	}
}

func (this *ContactList) StopConnections() {
	for _, contact := range this.Contacts() {
		contact.StopConnection()
	}
}
