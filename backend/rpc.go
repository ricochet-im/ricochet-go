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
