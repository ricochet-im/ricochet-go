package connection

import (
	"crypto/rsa"
	"github.com/s-rah/go-ricochet/channels"
	"github.com/s-rah/go-ricochet/policies"
	"github.com/s-rah/go-ricochet/utils"
	"sync"
)

// InboundConnectionHandler is a convieniance wrapper for handling inbound
// connections
type InboundConnectionHandler struct {
	connection *Connection
}

// HandleInboundConnection returns an InboundConnectionHandler given a connection
func HandleInboundConnection(c *Connection) *InboundConnectionHandler {
	ich := new(InboundConnectionHandler)
	ich.connection = c
	return ich
}

// ProcessAuthAsServer blocks until authentication has succeeded, failed, or the
// connection is closed. A non-nil error is returned in all cases other than successful
// and accepted authentication.
//
// ProcessAuthAsServer cannot be called at the same time as any other call to a Process
// function. Another Process function must be called after this function successfully
// returns to continue handling connection events.
//
// The acceptCallback function is called after receiving a valid authentication proof
// with the client's authenticated hostname and public key. acceptCallback must return
// true to accept authentication and allow the connection to continue, and also returns a
// boolean indicating whether the contact is known and recognized. Unknown contacts will
// assume they are required to send a contact request before any other activity.
func (ich *InboundConnectionHandler) ProcessAuthAsServer(privateKey *rsa.PrivateKey, sach func(hostname string, publicKey rsa.PublicKey) (allowed, known bool)) error {

	if privateKey == nil {
		return utils.PrivateKeyNotSetError
	}

	var breakOnce sync.Once

	var authAllowed, authKnown bool
	var authHostname string

	onAuthValid := func(hostname string, publicKey rsa.PublicKey) (allowed, known bool) {
		authAllowed, authKnown = sach(hostname, publicKey)
		if authAllowed {
			authHostname = hostname
		}
		breakOnce.Do(func() { go ich.connection.Break() })
		return authAllowed, authKnown
	}
	onAuthInvalid := func(err error) {
		// err is ignored at the moment
		breakOnce.Do(func() { go ich.connection.Break() })
	}

	ach := new(AutoConnectionHandler)
	ach.Init()
	ach.RegisterChannelHandler("im.ricochet.auth.hidden-service",
		func() channels.Handler {
			return &channels.HiddenServiceAuthChannel{
				PrivateKey:        privateKey,
				ServerAuthValid:   onAuthValid,
				ServerAuthInvalid: onAuthInvalid,
			}
		})

	// Ensure that the call to Process() cannot outlive this function,
	// particularly for the case where the policy timeout expires
	defer breakOnce.Do(func() { ich.connection.Break() })
	policy := policies.UnknownPurposeTimeout
	err := policy.ExecuteAction(func() error {
		return ich.connection.Process(ach)
	})

	if err == nil {
		if authAllowed == true {
			ich.connection.RemoteHostname = authHostname
			return nil
		}
		return utils.ClientFailedToAuthenticateError
	}

	return err
}
