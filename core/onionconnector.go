package core

import (
	"errors"
	"github.com/ricochet-im/ricochet-go/rpc"
	"golang.org/x/net/context"
	"log"
	"math/rand"
	"net"
	"time"
)

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
// instance. The backoff counter is __not__ reset after Connect returns.
//
// If the Network is not ready, this function will wait until the network
// becomes ready. The backoff attempt counter is automatically reset when
// the network status changes.
//
// Context can be used to set a timeout, deadline, or cancel function for
// the overall connection attempts.
func (oc *OnionConnector) Connect(address string, c context.Context) (net.Conn, error) {
	if oc.Network == nil {
		return nil, errors.New("No network configured for connector")
	}

	host, _, err := net.SplitHostPort(address)
	if err != nil || !IsOnionValid(host) {
		return nil, errors.New("Invalid address")
	}

	options := &net.Dialer{
		Cancel:  c.Done(),
		Timeout: 60 * time.Second, // Maximum time for a single connect attempt
	}

	// Internal context used by blocking functions, assigned in the loop
	var waitCtx context.Context
	cancelWaitFunc := func() {}
	// Cancel at return, extra closure required to call the current value
	defer func() { cancelWaitFunc() }()

	// Monitor for network connection status changes. On any change, reset backoff
	// and call cancelWaitFunc, which is set with waitCtx in the loop below, to cancel
	// the current wait and try again.
	networkMonitor := oc.Network.EventMonitor().Subscribe(20)
	defer oc.Network.EventMonitor().Unsubscribe(networkMonitor)
	go func() {
		var prevConnStatus ricochet.TorConnectionStatus_Status

		for {
			select {
			case <-c.Done():
				return
			case v, ok := <-networkMonitor:
				if !ok {
					// monitor channel is closed when Connect() returns
					return
				}

				event := v.(ricochet.NetworkStatus)

				var connStatus ricochet.TorConnectionStatus_Status
				if event.Connection != nil {
					connStatus = event.Connection.Status
				}
				if connStatus != prevConnStatus {
					prevConnStatus = connStatus
					oc.ResetBackoff()
					cancelWaitFunc()
				}
			}
		}
	}()

	for {
		waitCtx, cancelWaitFunc = context.WithCancel(c)

		proxy, err := oc.Network.WaitForProxyDialer(options, waitCtx)
		if err != nil {
			if c.Err() != nil {
				return nil, c.Err()
			} else if waitCtx.Err() != nil {
				continue
			} else {
				return nil, err
			}
		}

		conn, err := proxy.Dial("tcp", address)
		if err == nil {
			// Success!
			return conn, nil
		} else if c.Err() != nil {
			return nil, c.Err()
		} else if !oc.NeverGiveUp {
			return nil, err
		}

		log.Printf("Connection attempt %d to %s failed: %s", oc.AttemptCount, address, err)

		if err := oc.Backoff(waitCtx); err != nil {
			if c.Err() != nil {
				return nil, c.Err()
			} else if waitCtx.Err() != nil {
				continue
			} else {
				return nil, err
			}
		}
	}
}

var backoffDelay [7]int = [7]int{0, 30, 60, 120, 300, 600, 900}

func (oc *OnionConnector) Backoff(c context.Context) error {
	oc.AttemptCount++

	var delay int
	if oc.AttemptCount < len(backoffDelay) {
		delay = backoffDelay[oc.AttemptCount]
	} else {
		delay = backoffDelay[len(backoffDelay)-1]
	}
	// Jitter by +/-20%
	delay += int(float32(delay) * (rand.Float32()*0.4 - 0.2))

	waitCtx, finish := context.WithTimeout(c, time.Duration(delay)*time.Second)
	defer finish()
	<-waitCtx.Done()
	return c.Err()
}

func (oc *OnionConnector) ResetBackoff() {
	oc.AttemptCount = 0
}
