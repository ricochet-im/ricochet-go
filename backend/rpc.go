package main

import (
	"errors"
	ricochet "github.com/special/notricochet/core"
	rpc "github.com/special/notricochet/rpc"
	"golang.org/x/net/context"
	"log"
	"time"
)

var NotImplementedError error = errors.New("Not implemented")

type RpcServer struct {
	core *ricochet.Ricochet
}

func (s *RpcServer) GetServerStatus(ctx context.Context, req *rpc.ServerStatusRequest) (*rpc.ServerStatusReply, error) {
	if req.RpcVersion != 1 {
		return nil, errors.New("Unsupported RPC protocol version")
	}

	return &rpc.ServerStatusReply{
		RpcVersion:    1,
		ServerVersion: "0.0.0",
	}, nil
}

func (s *RpcServer) MonitorNetwork(req *rpc.MonitorNetworkRequest, stream rpc.RicochetCore_MonitorNetworkServer) error {
	events := s.core.Network.EventMonitor().Subscribe(20)
	defer s.core.Network.EventMonitor().Unsubscribe(events)

	// Send initial status event
	{
		event := s.core.Network.GetStatus()
		if err := stream.Send(&event); err != nil {
			return err
		}
	}

	for {
		event, ok := (<-events).(rpc.NetworkStatus)
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

func (s *RpcServer) StartNetwork(ctx context.Context, req *rpc.StartNetworkRequest) (*rpc.NetworkStatus, error) {
	// err represents the result of the first connection attempt, but as long
	// as 'ok' is true, the network has started and this call was successful.
	ok, err := s.core.Network.Start("tcp://127.0.0.1:9051", "")
	if !ok {
		return nil, err
	}

	status := s.core.Network.GetStatus()
	return &status, nil
}

func (s *RpcServer) StopNetwork(ctx context.Context, req *rpc.StopNetworkRequest) (*rpc.NetworkStatus, error) {
	s.core.Network.Stop()
	status := s.core.Network.GetStatus()
	return &status, nil
}

func (s *RpcServer) GetIdentity(ctx context.Context, req *rpc.IdentityRequest) (*rpc.Identity, error) {
	reply := rpc.Identity{
		Address: s.core.Identity.Address(),
	}
	return &reply, nil
}

func (s *RpcServer) MonitorContacts(req *rpc.MonitorContactsRequest, stream rpc.RicochetCore_MonitorContactsServer) error {
	monitor := s.core.Identity.ContactList().EventMonitor().Subscribe(20)
	defer s.core.Identity.ContactList().EventMonitor().Unsubscribe(monitor)

	// Populate
	contacts := s.core.Identity.ContactList().Contacts()
	for _, contact := range contacts {
		data := &rpc.Contact{
			Id:            int32(contact.Id()),
			Address:       contact.Address(),
			Nickname:      contact.Nickname(),
			WhenCreated:   contact.WhenCreated().Format(time.RFC3339),
			LastConnected: contact.LastConnected().Format(time.RFC3339),
		}
		event := &rpc.ContactEvent{
			Type: rpc.ContactEvent_POPULATE,
			Subject: &rpc.ContactEvent_Contact{
				Contact: data,
			},
		}
		if err := stream.Send(event); err != nil {
			return err
		}
	}
	// Terminate populate list with a null subject
	{
		event := &rpc.ContactEvent{
			Type: rpc.ContactEvent_POPULATE,
		}
		if err := stream.Send(event); err != nil {
			return err
		}
	}

	for {
		event, ok := (<-monitor).(rpc.ContactEvent)
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

func (s *RpcServer) AddContactRequest(ctx context.Context, req *rpc.ContactRequest) (*rpc.Contact, error) {
	return nil, NotImplementedError
}

func (s *RpcServer) UpdateContact(ctx context.Context, req *rpc.Contact) (*rpc.Contact, error) {
	return nil, NotImplementedError
}

func (s *RpcServer) DeleteContact(ctx context.Context, req *rpc.DeleteContactRequest) (*rpc.DeleteContactReply, error) {
	contactList := s.core.Identity.ContactList()
	contact := contactList.ContactByAddress(req.Address)
	if contact == nil || (req.Id != 0 && contact.Id() != int(req.Id)) {
		return nil, errors.New("Contact not found")
	}

	if err := contactList.RemoveContact(contact); err != nil {
		return nil, err
	}

	return &rpc.DeleteContactReply{}, nil
}

func (s *RpcServer) AcceptInboundRequest(ctx context.Context, req *rpc.ContactRequest) (*rpc.Contact, error) {
	return nil, NotImplementedError
}

func (s *RpcServer) RejectInboundRequest(ctx context.Context, req *rpc.ContactRequest) (*rpc.RejectInboundRequestReply, error) {
	return nil, NotImplementedError
}

func (s *RpcServer) StreamConversations(stream rpc.RicochetCore_StreamConversationsServer) error {
	return NotImplementedError
}
