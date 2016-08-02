package main

import (
	"errors"
	rpc "github.com/special/ricochet-go/rpc"
	"golang.org/x/net/context"
	"time"
)

type RicochetCore struct {
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
	status := &rpc.NetworkStatus{
		Control: &rpc.TorControlStatus{
			Status:       rpc.TorControlStatus_ERROR,
			ErrorMessage: "Not implemented",
		},
	}

	for i := 0; i < 100; i++ {
		if err := stream.Send(status); err != nil {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

func (core *RicochetCore) StartNetwork(ctx context.Context, req *rpc.StartNetworkRequest) (*rpc.NetworkStatus, error) {
	return nil, errors.New("Not implemented")
}

func (core *RicochetCore) StopNetwork(ctx context.Context, req *rpc.StopNetworkRequest) (*rpc.NetworkStatus, error) {
	return nil, errors.New("Not implemented")
}
