package application

import (
	"crypto/rsa"
	"github.com/yawning/bulb"
	"net"
)

// "127.0.0.1:9051" "tcp4"
// "/var/run/tor/control" "unix"
func SetupOnion(torControlAddress string, torControlSocketType string, authentication string, pk *rsa.PrivateKey, onionport uint16) (net.Listener, error) {
	c, err := bulb.Dial(torControlSocketType, torControlAddress)
	if err != nil {
		return nil, err
	}

	if err := c.Authenticate(authentication); err != nil {
		return nil, err
	}

	cfg := &bulb.NewOnionConfig{
		DiscardPK:  true,
		PrivateKey: pk,
	}

	return c.NewListener(cfg, onionport)
}
