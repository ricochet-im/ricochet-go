package main

import (
	"errors"
	ricochet "github.com/special/notricochet/core"
	rpc "github.com/special/notricochet/rpc"
	"golang.org/x/net/context"
	"log"
)

type RicochetCore struct {
	Network *ricochet.Network
	Config  *ricochet.Config
}

var NotImplementedError error = errors.New("Not implemented")

func (core *RicochetCore) GetServerStatus(ctx context.Context, req *rpc.ServerStatusRequest) (*rpc.ServerStatusReply, error) {
	if req.RpcVersion != 1 {
		return nil, errors.New("Unsupported RPC protocol version")
	}

	return &rpc.ServerStatusReply{
		RpcVersion:    1,
		ServerVersion: "0.0.0",
	}, nil
}

func (core *RicochetCore) MonitorNetwork(req *rpc.MonitorNetworkRequest, stream rpc.RicochetCore_MonitorNetworkServer) error {
	events := core.Network.EventMonitor().Subscribe(20)
	defer core.Network.EventMonitor().Unsubscribe(events)

	// Send initial status event
	{
		event := core.Network.GetStatus()
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

func (core *RicochetCore) StartNetwork(ctx context.Context, req *rpc.StartNetworkRequest) (*rpc.NetworkStatus, error) {
	// err represents the result of the first connection attempt, but as long
	// as 'ok' is true, the network has started and this call was successful.
	ok, err := core.Network.Start("tcp://127.0.0.1:9051", "")
	if !ok {
		return nil, err
	}

	status := core.Network.GetStatus()
	return &status, nil
}

func (core *RicochetCore) StopNetwork(ctx context.Context, req *rpc.StopNetworkRequest) (*rpc.NetworkStatus, error) {
	core.Network.Stop()
	status := core.Network.GetStatus()
	return &status, nil
}

func (core *RicochetCore) GetIdentity(ctx context.Context, req *rpc.IdentityRequest) (*rpc.Identity, error) {
	return nil, NotImplementedError
}

func (core *RicochetCore) MonitorContacts(req *rpc.MonitorContactsRequest, stream rpc.RicochetCore_MonitorContactsServer) error {
	return NotImplementedError
}

func (core *RicochetCore) AddContactRequest(ctx context.Context, req *rpc.ContactRequest) (*rpc.Contact, error) {
	return nil, NotImplementedError
}

func (core *RicochetCore) UpdateContact(ctx context.Context, req *rpc.Contact) (*rpc.Contact, error) {
	return nil, NotImplementedError
}

func (core *RicochetCore) DeleteContact(ctx context.Context, req *rpc.DeleteContactRequest) (*rpc.DeleteContactReply, error) {
	return nil, NotImplementedError
}

func (core *RicochetCore) AcceptInboundRequest(ctx context.Context, req *rpc.ContactRequest) (*rpc.Contact, error) {
	return nil, NotImplementedError
}

func (core *RicochetCore) RejectInboundRequest(ctx context.Context, req *rpc.ContactRequest) (*rpc.RejectInboundRequestReply, error) {
	return nil, NotImplementedError
}

func (core *RicochetCore) StreamConversations(stream rpc.RicochetCore_StreamConversationsServer) error {
	return NotImplementedError
}
