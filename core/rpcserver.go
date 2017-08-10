package core

import (
	"errors"
	"github.com/ricochet-im/ricochet-go/rpc"
	"golang.org/x/net/context"
	"log"
)

var NotImplementedError error = errors.New("Not implemented")

type RpcServer struct {
	Core *Ricochet
}

func (s *RpcServer) GetServerStatus(ctx context.Context, req *ricochet.ServerStatusRequest) (*ricochet.ServerStatusReply, error) {
	if req.RpcVersion != 1 {
		return nil, errors.New("Unsupported RPC protocol version")
	}

	return &ricochet.ServerStatusReply{
		RpcVersion:    1,
		ServerVersion: "0.0.0",
	}, nil
}

func (s *RpcServer) MonitorNetwork(req *ricochet.MonitorNetworkRequest, stream ricochet.RicochetCore_MonitorNetworkServer) error {
	events := s.Core.Network.EventMonitor().Subscribe(20)
	defer s.Core.Network.EventMonitor().Unsubscribe(events)

	// Send initial status event
	{
		event := s.Core.Network.GetStatus()
		if err := stream.Send(&event); err != nil {
			return err
		}
	}

	for {
		event, ok := (<-events).(ricochet.NetworkStatus)
		if !ok {
			break
		}

		log.Printf("RPC monitor event: %v", event)
		if err := stream.Send(&event); err != nil {
			return err
		}
	}

	return nil
}

func (s *RpcServer) StartNetwork(ctx context.Context, req *ricochet.StartNetworkRequest) (*ricochet.NetworkStatus, error) {
	// err represents the result of the first connection attempt, but as long
	// as 'ok' is true, the network has started and this call was successful.
	ok, err := s.Core.Network.Start()
	if !ok {
		return nil, err
	}

	status := s.Core.Network.GetStatus()
	return &status, nil
}

func (s *RpcServer) StopNetwork(ctx context.Context, req *ricochet.StopNetworkRequest) (*ricochet.NetworkStatus, error) {
	s.Core.Network.Stop()
	status := s.Core.Network.GetStatus()
	return &status, nil
}

func (s *RpcServer) GetIdentity(ctx context.Context, req *ricochet.IdentityRequest) (*ricochet.Identity, error) {
	reply := ricochet.Identity{
		Address: s.Core.Identity.Address(),
	}
	return &reply, nil
}

func (s *RpcServer) MonitorContacts(req *ricochet.MonitorContactsRequest, stream ricochet.RicochetCore_MonitorContactsServer) error {
	monitor := s.Core.Identity.ContactList().EventMonitor().Subscribe(20)
	defer s.Core.Identity.ContactList().EventMonitor().Unsubscribe(monitor)

	// Populate
	contacts := s.Core.Identity.ContactList().Contacts()
	for _, contact := range contacts {
		event := &ricochet.ContactEvent{
			Type: ricochet.ContactEvent_POPULATE,
			Subject: &ricochet.ContactEvent_Contact{
				Contact: contact.Data(),
			},
		}
		if err := stream.Send(event); err != nil {
			return err
		}
	}
	// Terminate populate list with a null subject
	{
		event := &ricochet.ContactEvent{
			Type: ricochet.ContactEvent_POPULATE,
		}
		if err := stream.Send(event); err != nil {
			return err
		}
	}

	for {
		event, ok := (<-monitor).(ricochet.ContactEvent)
		if !ok {
			break
		}

		log.Printf("Contact event: %v", event)
		if err := stream.Send(&event); err != nil {
			return err
		}
	}

	return nil
}

func (s *RpcServer) AddContactRequest(ctx context.Context, req *ricochet.ContactRequest) (*ricochet.Contact, error) {
	contactList := s.Core.Identity.ContactList()
	if req.Direction != ricochet.ContactRequest_OUTBOUND {
		return nil, errors.New("Request must be outbound")
	}

	contact, err := contactList.AddContactRequest(req.Address, req.Nickname, req.FromNickname, req.Text)
	if err != nil {
		return nil, err
	}

	return contact.Data(), nil
}

func (s *RpcServer) UpdateContact(ctx context.Context, req *ricochet.Contact) (*ricochet.Contact, error) {
	return nil, NotImplementedError
}

func (s *RpcServer) DeleteContact(ctx context.Context, req *ricochet.DeleteContactRequest) (*ricochet.DeleteContactReply, error) {
	contactList := s.Core.Identity.ContactList()
	contact := contactList.ContactByAddress(req.Address)
	if contact == nil || (req.Id != 0 && contact.Id() != int(req.Id)) {
		return nil, errors.New("Contact not found")
	}

	if err := contactList.RemoveContact(contact); err != nil {
		return nil, err
	}

	return &ricochet.DeleteContactReply{}, nil
}

func (s *RpcServer) AcceptInboundRequest(ctx context.Context, req *ricochet.ContactRequest) (*ricochet.Contact, error) {
	if req.Direction != ricochet.ContactRequest_INBOUND {
		return nil, errors.New("Request must be inbound")
	}
	contactList := s.Core.Identity.ContactList()
	request := contactList.InboundRequestByAddress(req.Address)
	if request == nil {
		return nil, errors.New("Request does not exist")
	}
	contact, err := request.Accept()
	if err != nil {
		return nil, err
	}
	return contact.Data(), nil
}

func (s *RpcServer) RejectInboundRequest(ctx context.Context, req *ricochet.ContactRequest) (*ricochet.RejectInboundRequestReply, error) {
	if req.Direction != ricochet.ContactRequest_INBOUND {
		return nil, errors.New("Request must be inbound")
	}
	contactList := s.Core.Identity.ContactList()
	request := contactList.InboundRequestByAddress(req.Address)
	if request == nil {
		return nil, errors.New("Request does not exist")
	}
	request.Reject()
	return &ricochet.RejectInboundRequestReply{}, nil
}

func (s *RpcServer) MonitorConversations(req *ricochet.MonitorConversationsRequest, stream ricochet.RicochetCore_MonitorConversationsServer) error {
	// XXX Technically there is a race between starting to monitor
	// and the list and state of messages used to populate, that could
	// result in duplicate messages or other weird behavior.
	// Same problem exists for other places this pattern is used.
	monitor := s.Core.Identity.ConversationStream.Subscribe(100)
	defer s.Core.Identity.ConversationStream.Unsubscribe(monitor)

	{
		// Populate with existing conversations
		contacts := s.Core.Identity.ContactList().Contacts()
		for _, contact := range contacts {
			messages := contact.Conversation().Messages()
			for _, message := range messages {
				event := ricochet.ConversationEvent{
					Type: ricochet.ConversationEvent_POPULATE,
					Msg:  message,
				}
				if err := stream.Send(&event); err != nil {
					return err
				}
			}
		}

		// End population with an empty populate event
		event := ricochet.ConversationEvent{
			Type: ricochet.ConversationEvent_POPULATE,
		}
		if err := stream.Send(&event); err != nil {
			return err
		}
	}

	for {
		event, ok := (<-monitor).(ricochet.ConversationEvent)
		if !ok {
			break
		}

		if err := stream.Send(&event); err != nil {
			return err
		}
	}

	return nil
}

func (s *RpcServer) SendMessage(ctx context.Context, req *ricochet.Message) (*ricochet.Message, error) {
	if req.Sender == nil || !req.Sender.IsSelf {
		return nil, errors.New("Invalid message sender")
	} else if req.Recipient == nil || req.Recipient.IsSelf {
		return nil, errors.New("Invalid message recipient")
	}

	contact := s.Core.Identity.ContactList().ContactByAddress(req.Recipient.Address)
	if contact == nil || (req.Recipient.ContactId != 0 && int32(contact.Id()) != req.Recipient.ContactId) {
		return nil, errors.New("Unknown recipient")
	}

	// XXX timestamp
	// XXX validate text
	// XXX identifier

	message, err := contact.Conversation().Send(req.Text)
	if err != nil {
		return nil, err
	}

	return message, nil
}

func (s *RpcServer) MarkConversationRead(ctx context.Context, req *ricochet.MarkConversationReadRequest) (*ricochet.Reply, error) {
	if req.Entity == nil || req.Entity.IsSelf {
		return nil, errors.New("Invalid entity")
	}

	contact := s.Core.Identity.ContactList().ContactByAddress(req.Entity.Address)
	if contact == nil || (req.Entity.ContactId != 0 && int32(contact.Id()) != req.Entity.ContactId) {
		return nil, errors.New("Unknown entity")
	}

	contact.Conversation().MarkReadBeforeMessage(req.LastRecvIdentifier)
	return &ricochet.Reply{}, nil
}
