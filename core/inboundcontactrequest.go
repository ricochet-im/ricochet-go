package core

import (
	"errors"
	"github.com/ricochet-im/ricochet-go/rpc"
	channels "github.com/s-rah/go-ricochet/channels"
	connection "github.com/s-rah/go-ricochet/connection"
	"log"
	"sync"
	"time"
)

type InboundContactRequest struct {
	core    *Ricochet
	mutex   sync.Mutex
	data    ricochet.ContactRequest
	Address string

	// If non-nil, sent the new contact on accept or nil on reject.
	// Used to signal back to active connections when request state changes.
	contactResultChan chan *Contact

	// Called when the request state is changed
	StatusChanged func(request *InboundContactRequest)
}

type inboundRequestHandler struct {
	Name    string
	Message string

	RequestReceivedChan chan struct{}
	ResponseChan        chan string
}

func (irh *inboundRequestHandler) ContactRequestRejected() {}
func (irh *inboundRequestHandler) ContactRequestAccepted() {}
func (irh *inboundRequestHandler) ContactRequestError()    {}
func (irh *inboundRequestHandler) ContactRequest(name, message string) string {
	if len(name) > 0 && !IsNicknameAcceptable(name) {
		log.Printf("protocol: Stripping unacceptable nickname from inbound request; encoded: %x", []byte(name))
		name = ""
	}
	irh.Name = name
	if len(message) > 0 && !IsMessageAcceptable(message) {
		log.Printf("protocol: Stripping unacceptable message from inbound request; len: %d, encoded: %x", len(message), []byte(message))
		message = ""
	}
	irh.Message = message

	// Signal to the goroutine that a request was received
	irh.RequestReceivedChan <- struct{}{}
	// Wait for a response
	response := <-irh.ResponseChan
	return response
}

// HandleInboundRequestConnection takes an authenticated connection that does not
// associate to any known contact and handles inbound contact request channels.
// If no request is seen after a short timeout, the connection will be closed.
// If a valid request is received, the connection will stay open to wait for the
// user's reply.
//
// This function takes full ownership of the connection. On return, the connection
// will be either closed and a non-nil error returned, or will be assigned to an
// existing Contact along with a nil return value.
func HandleInboundRequestConnection(conn *connection.Connection, contactList *ContactList) error {
	log.Printf("Handling inbound contact request connection")
	ach := &connection.AutoConnectionHandler{}
	ach.Init()

	// There may only ever be one contact request channel on the connection
	req := &inboundRequestHandler{
		RequestReceivedChan: make(chan struct{}),
		ResponseChan:        make(chan string),
	}
	// XXX should close conn if the channel goes away...
	ach.RegisterChannelHandler("im.ricochet.contact.request", func() channels.Handler {
		return &channels.ContactRequestChannel{Handler: req}
	})

	processChan := make(chan error)
	go func() {
		processChan <- conn.Process(ach)
	}()

	address, ok := AddressFromPlainHost(conn.RemoteHostname)
	if !ok {
		conn.Conn.Close()
		return <-processChan
	}

	// Expecting to receive request data within 15 seconds
	select {
	case <-req.RequestReceivedChan:
		break
	case err := <-processChan:
		if err == nil {
			conn.Conn.Close()
			err = errors.New("unexpected break")
		}
		return err
	case <-time.After(15 * time.Second):
		// Didn't receive a contact request fast enough
		conn.Conn.Close()
		return <-processChan
	}

	// Function to respond to the request; changed after the initial response
	respond := func(status string) { req.ResponseChan <- status }

	request, contact := contactList.AddOrUpdateInboundContactRequest(address, req.Name, req.Message)
	if contact == nil && request != nil && !request.IsRejected() {
		// Pending request; keep connection open and wait for a user response
		respond("Pending")
		contactChan := request.getContactResultChannel()
		select {
		case c, ok := <-contactChan:
			if !ok {
				// Replaced by a different connection or otherwise cancelled without reply
				conn.Conn.Close()
				return <-processChan
			}
			// Set contact and fall out to handle the reply
			contact = c
			break
		case err := <-processChan:
			request.clearContactResultChannel(contactChan)
			for {
				_, ok := <-contactChan
				if !ok {
					break
				}
			}
			return err
		}

		// Change how future responses are sent
		respond = func(status string) {
			conn.Do(func() error {
				channel := conn.Channel("im.ricochet.contact.request", channels.Inbound)
				if channel == nil {
					return errors.New("no channel")
				}
				channel.Handler.(*channels.ContactRequestChannel).SendResponse(status)
				// Also close the channel; this was a final response
				channel.CloseChannel()
				return nil
			})
		}
	}

	// Have a response (either immediately or after pending)
	if contact != nil {
		// Accepted
		respond("Accepted")
		if err := conn.Break(); err != nil {
			// Connection lost; but request was accepted anyway, so it'll get reconnected later
			return err
		}
		contact.AssignConnection(conn)
		return nil
	} else {
		// Rejected
		respond("Rejected")
		if request != nil {
			contactList.RemoveInboundContactRequest(request)
		}
		conn.Conn.Close()
		return <-processChan
	}
}

// CreateInboundContactRequest constructs a new InboundContactRequest, usually from a newly
// received request on an open connection. Requests are managed through the ContactList, so
// generally you should use ContactList.AddOrUpdateInboundContactRequest instead of calling
// this function directly.
func CreateInboundContactRequest(core *Ricochet, address, nickname, message string) *InboundContactRequest {
	cr := &InboundContactRequest{
		core: core,
		data: ricochet.ContactRequest{
			Direction:    ricochet.ContactRequest_INBOUND,
			Address:      address,
			Text:         message,
			FromNickname: nickname,
			WhenCreated:  time.Now().Format(time.RFC3339),
		},
		Address: address,
	}

	return cr
}

// getContactResultChannel returns a channel that will be sent a Contact if the request is
// accepted, nil if the request is rejected, or closed if the channel is no longer used.
// This is used to communciate with active connections for pending requests.
func (cr *InboundContactRequest) getContactResultChannel() chan *Contact {
	cr.mutex.Lock()
	defer cr.mutex.Unlock()

	if cr.contactResultChan != nil {
		close(cr.contactResultChan)
	}
	cr.contactResultChan = make(chan *Contact)
	return cr.contactResultChan
}

func (cr *InboundContactRequest) clearContactResultChannel(c chan *Contact) {
	cr.mutex.Lock()
	defer cr.mutex.Unlock()
	if cr.contactResultChan == c {
		close(cr.contactResultChan)
		cr.contactResultChan = nil
	}
}

func (cr *InboundContactRequest) CloseConnection() {
	cr.mutex.Lock()
	defer cr.mutex.Unlock()
	if cr.contactResultChan != nil {
		close(cr.contactResultChan)
		cr.contactResultChan = nil
	}
}

func (cr *InboundContactRequest) Update(nickname, message string) {
	cr.mutex.Lock()
	defer cr.mutex.Unlock()

	if cr.data.Rejected {
		return
	}

	// These should already be validated, but just in case..
	if len(nickname) == 0 || IsNicknameAcceptable(nickname) {
		cr.data.FromNickname = nickname
	}
	if len(message) == 0 || IsMessageAcceptable(message) {
		cr.data.Text = message
	}

	if cr.StatusChanged != nil {
		cr.StatusChanged(cr)
	}
}

func (cr *InboundContactRequest) SetNickname(nickname string) error {
	cr.mutex.Lock()
	defer cr.mutex.Unlock()

	if IsNicknameAcceptable(nickname) {
		cr.data.FromNickname = nickname
	} else {
		return errors.New("Invalid nickname")
	}
	return nil
}

func (cr *InboundContactRequest) Accept() (*Contact, error) {
	cr.mutex.Lock()
	defer cr.mutex.Unlock()

	if cr.data.Rejected {
		log.Printf("Accept called on an inbound contact request that was already rejected; request is %v", cr)
		return nil, errors.New("Contact request has already been rejected")
	}

	log.Printf("Accepting contact request from %s", cr.data.Address)

	data := &ricochet.Contact{
		Address:     cr.data.Address,
		Nickname:    cr.data.FromNickname,
		WhenCreated: cr.data.WhenCreated,
	}
	contact, err := cr.core.Identity.ContactList().AddNewContact(data)
	if err != nil {
		log.Printf("Error occurred in accepting contact request: %s", err)
		return nil, err
	}

	if err := cr.acceptWithContact(contact); err != nil {
		return contact, err
	}
	return contact, nil
}

func (cr *InboundContactRequest) AcceptWithContact(contact *Contact) error {
	cr.mutex.Lock()
	defer cr.mutex.Unlock()
	return cr.acceptWithContact(contact)
}

// Assumes mutex
func (cr *InboundContactRequest) acceptWithContact(contact *Contact) error {
	if contact.Address() != cr.data.Address {
		return errors.New("Contact address does not match request in accept")
	}

	// Send to active connection if present
	if cr.contactResultChan != nil {
		cr.contactResultChan <- contact
		close(cr.contactResultChan)
		cr.contactResultChan = nil
	}

	cr.core.Identity.ContactList().RemoveInboundContactRequest(cr)
	return nil
}

func (cr *InboundContactRequest) Reject() {
	cr.mutex.Lock()
	defer cr.mutex.Unlock()

	if cr.data.Rejected {
		return
	}

	log.Printf("Rejecting contact request from %s", cr.data.Address)
	cr.data.Rejected = true

	// Signal to the active connection
	if cr.contactResultChan != nil {
		cr.contactResultChan <- nil
		close(cr.contactResultChan)
		cr.contactResultChan = nil
	}

	// Signal update to the callback (probably from ContactList)
	if cr.StatusChanged != nil {
		cr.StatusChanged(cr)
	}

	cr.core.Identity.ContactList().RemoveInboundContactRequest(cr)
}

func (cr *InboundContactRequest) Data() ricochet.ContactRequest {
	return cr.data
}

func (cr *InboundContactRequest) IsRejected() bool {
	cr.mutex.Lock()
	defer cr.mutex.Unlock()
	return cr.data.Rejected
}
