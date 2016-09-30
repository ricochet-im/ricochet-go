package core

import (
	"errors"
	"golang.org/x/net/context"
	"log"
	"net"
	"strings"
	"time"
)

// XXX on failures, decide how long to wait before trying again
// XXX limit and queue global attempts?
// XXX circ/stream/fetch monitoring

type OnionConnector struct {
	Network      *Network
	NeverGiveUp  bool
	AttemptCount int
}

// Attempt to connect to 'address', which must be a .onion address and port,
// using the Network from this OnionConnector instance.
//
// If NeverGiveUp is set, failed connections will be retried automatically,
// with appropriate backoff periods, and an error is only returned in fatal
// situations. The backoff is defined by AttemptCount on the OnionConnector
// instance -- it is not reset after Connect returns.
//
// If the Network is not ready, this function will wait and monitor the
// Network's status.
//
// Context can be used to set a timeout, deadline, or cancel function for
// the connection attempt.
func (oc *OnionConnector) Connect(address string, c context.Context) (net.Conn, error) {
	if oc.Network == nil {
		return nil, errors.New("No network configured for connector")
	}

	host, _, err := net.SplitHostPort(address)
	if err != nil || !strings.HasSuffix(host, ".onion") {
		return nil, errors.New("Invalid address")
	}

	options := &net.Dialer{
		Cancel:  c.Done(),
		Timeout: 0, // XXX should use timeout
	}

	for {
		// XXX This waits to know SOCKS info, but does not wait for connection
		// ready state; should it?
		proxy, err := oc.Network.WaitForProxyDialer(options, c)
		if err != nil {
			return nil, err
		}

		conn, err := proxy.Dial("tcp", address)
		if err != nil {
			if c.Err() != nil {
				return nil, c.Err()
			}
			if !oc.NeverGiveUp {
				return nil, err
			}

			log.Printf("Connection attempt to %s failed: %s", address, err)
			if err := oc.Backoff(c); err != nil {
				return nil, err
			}

			continue
		}

		return conn, nil
	}
}

func (oc *OnionConnector) Backoff(c context.Context) error {
	oc.AttemptCount++
	// XXX This should actually do backoff and be less dumb
	waitCtx, finish := context.WithTimeout(c, 10*time.Second)
	defer finish()
	<-waitCtx.Done()
	return c.Err()
}
