package core

import (
	"errors"
	"github.com/ricochet-im/ricochet-go/rpc"
	protocol "github.com/s-rah/go-ricochet"
	"log"
	"sync"
	"time"
)

type InboundContactRequest struct {
	core      *Ricochet
	mutex     sync.Mutex
	data      ricochet.ContactRequest
	conn      *protocol.OpenConnection
	channelID int32
	Address   string

	// Called when the request state is changed
	StatusChanged func(request *InboundContactRequest)
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

// XXX There should be stricter management & a timeout for this connection
func (cr *InboundContactRequest) SetConnection(conn *protocol.OpenConnection, channelID int32) {
	cr.mutex.Lock()
	defer cr.mutex.Unlock()

	if cr.conn != nil && cr.conn != conn {
		log.Printf("Replacing connection on an inbound contact request")
		cr.conn.Close()
	}
	cr.conn = conn
	cr.channelID = channelID
}

func (cr *InboundContactRequest) CloseConnection() {
	if cr.conn != nil {
		cr.conn.Close()
		cr.conn = nil
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

func (cr *InboundContactRequest) Accept() (*Contact, error) {
	cr.mutex.Lock()
	defer cr.mutex.Unlock()

	if cr.data.Rejected {
		log.Printf("Accept called on an inbound contact request that was already rejected; request is %v", cr)
		return nil, errors.New("Contact request has already been rejected")
	}

	log.Printf("Accepting contact request from %s", cr.data.Address)

	onion, _ := OnionFromAddress(cr.data.Address)
	configContact := ConfigContact{
		Hostname:    onion,
		Nickname:    cr.data.FromNickname,
		WhenCreated: cr.data.WhenCreated,
	}
	contact, err := cr.core.Identity.ContactList().AddNewContact(configContact)
	if err != nil {
		log.Printf("Error occurred in accepting contact request: %s", err)
		return nil, err
	}

	if err := cr.AcceptWithContact(contact); err != nil {
		return contact, err
	}
	return contact, nil
}

func (cr *InboundContactRequest) AcceptWithContact(contact *Contact) error {
	if contact.Address() != cr.data.Address {
		return errors.New("Contact address does not match request in accept")
	}

	cr.core.Identity.ContactList().RemoveInboundContactRequest(cr)

	// Pass the open connection to the new contact
	if cr.conn != nil && !cr.conn.Closed {
		cr.conn.AckContactRequest(cr.channelID, "Accepted")
		cr.conn.CloseChannel(cr.channelID)
		contact.OnConnectionAuthenticated(cr.conn, true)
		cr.conn = nil
	}

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

	// Signal update to the callback (probably from ContactList)
	if cr.StatusChanged != nil {
		cr.StatusChanged(cr)
	}

	if cr.conn != nil && !cr.conn.Closed {
		cr.conn.AckContactRequest(cr.channelID, "Rejected")
		cr.conn.CloseChannel(cr.channelID)
		cr.conn.Close()
		cr.conn = nil

		// The request can be removed once a protocol response is sent
		cr.core.Identity.ContactList().RemoveInboundContactRequest(cr)
	}
}

func (cr *InboundContactRequest) Data() ricochet.ContactRequest {
	return cr.data
}

func (cr *InboundContactRequest) IsRejected() bool {
	cr.mutex.Lock()
	defer cr.mutex.Unlock()
	return cr.data.Rejected
}
