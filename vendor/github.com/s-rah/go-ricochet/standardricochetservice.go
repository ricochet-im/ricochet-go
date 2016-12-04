package goricochet

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"github.com/s-rah/go-ricochet/utils"
	"io/ioutil"
	"log"
	"net"
	"strconv"
)

// StandardRicochetService implements all the necessary flows to implement a
// minimal, protocol compliant Ricochet Service. It can be built on by other
// applications to produce automated riochet applications, and is a useful
// example for other implementations.
type StandardRicochetService struct {
	PrivateKey     *rsa.PrivateKey
	serverHostname string
}

// StandardRicochetConnection implements the ConnectionHandler interface
// to handle events on connections. An instance of StandardRicochetConnection
// is created for each OpenConnection by the HandleConnection method.
type StandardRicochetConnection struct {
	Conn       *OpenConnection
	PrivateKey *rsa.PrivateKey
}

// Init initializes a StandardRicochetService with the cryptographic key given
// by filename.
func (srs *StandardRicochetService) Init(filename string) error {
	pemData, err := ioutil.ReadFile(filename)

	if err != nil {
		return errors.New("Could not setup ricochet service: could not read private key")
	}

	block, _ := pem.Decode(pemData)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		return errors.New("Could not setup ricochet service: no valid PEM data found")
	}

	srs.PrivateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return errors.New("Could not setup ricochet service: could not parse private key")
	}

	publicKeyBytes, _ := asn1.Marshal(rsa.PublicKey{
		N: srs.PrivateKey.PublicKey.N,
		E: srs.PrivateKey.PublicKey.E,
	})

	srs.serverHostname = utils.GetTorHostname(publicKeyBytes)
	log.Printf("Initialised ricochet service for %s", srs.serverHostname)

	return nil
}

// Listen starts listening for service connections on localhost `port`.
func (srs *StandardRicochetService) Listen(handler ServiceHandler, port int) {
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		log.Printf("Cannot Listen on Port %v", port)
		return
	}

	Serve(ln, handler)
}

// Connect initiates a new client connection to `hostname`, which must be in one
// of the forms accepted by the goricochet.Connect() method.
func (srs *StandardRicochetService) Connect(hostname string) (*OpenConnection, error) {
	log.Printf("Connecting to...%s", hostname)
	oc, err := Connect(hostname)
	if err != nil {
		return nil, errors.New("Could not connect to: " + hostname + " " + err.Error())
	}
	oc.MyHostname = srs.serverHostname
	return oc, nil
}

// OnNewConnection is called for new inbound connections to our service. This
// method implements the ServiceHandler interface.
func (srs *StandardRicochetService) OnNewConnection(oc *OpenConnection) {
	oc.MyHostname = srs.serverHostname
}

// OnFailedConnection is called for inbound connections that fail to successfully
// complete version negotiation for any reason. This method implements the
// ServiceHandler interface.
func (srs *StandardRicochetService) OnFailedConnection(err error) {
	log.Printf("Inbound connection failed: %s", err)
}

// ------

// OnReady is called when a client or server sucessfully passes Version Negotiation.
func (src *StandardRicochetConnection) OnReady(oc *OpenConnection) {
	src.Conn = oc
	if oc.Client {
		log.Printf("Successfully connected to %s", oc.OtherHostname)
		oc.IsAuthed = true // Connections to Servers are Considered Authenticated by Default
		oc.Authenticate(1)
	} else {
		log.Printf("Inbound connection received")
	}
}

// OnDisconnect is called when a connection is closed
func (src *StandardRicochetConnection) OnDisconnect() {
	log.Printf("Disconnected from %s", src.Conn.OtherHostname)
}

// OnAuthenticationRequest is called when a client requests Authentication
func (src *StandardRicochetConnection) OnAuthenticationRequest(channelID int32, clientCookie [16]byte) {
	src.Conn.ConfirmAuthChannel(channelID, clientCookie)
}

// OnAuthenticationChallenge constructs a valid authentication challenge to the serverCookie
func (src *StandardRicochetConnection) OnAuthenticationChallenge(channelID int32, serverCookie [16]byte) {
	// DER Encode the Public Key
	publickeyBytes, _ := asn1.Marshal(rsa.PublicKey{
		N: src.PrivateKey.PublicKey.N,
		E: src.PrivateKey.PublicKey.E,
	})
	src.Conn.SendProof(1, serverCookie, publickeyBytes, src.PrivateKey)
}

// OnAuthenticationProof is called when a client sends Proof for an existing authentication challenge
func (src *StandardRicochetConnection) OnAuthenticationProof(channelID int32, publicKey []byte, signature []byte) {
	result := src.Conn.ValidateProof(channelID, publicKey, signature)
	// This implementation always sends 'true', indicating that the contact is known
	src.Conn.SendAuthenticationResult(channelID, result, true)
	src.Conn.IsAuthed = result
	src.Conn.CloseChannel(channelID)
}

// OnAuthenticationResult is called once a server has returned the result of the Proof Verification
func (src *StandardRicochetConnection) OnAuthenticationResult(channelID int32, result bool, isKnownContact bool) {
	src.Conn.IsAuthed = result
}

// IsKnownContact allows a caller to determine if a hostname an authorized contact.
func (src *StandardRicochetConnection) IsKnownContact(hostname string) bool {
	return false
}

// OnContactRequest is called when a client sends a new contact request
func (src *StandardRicochetConnection) OnContactRequest(channelID int32, nick string, message string) {
}

// OnContactRequestAck is called when a server sends a reply to an existing contact request
func (src *StandardRicochetConnection) OnContactRequestAck(channelID int32, status string) {
}

// OnOpenChannelRequest is called when a client or server requests to open a new channel
func (src *StandardRicochetConnection) OnOpenChannelRequest(channelID int32, channelType string) {
	src.Conn.AckOpenChannel(channelID, channelType)
}

// OnOpenChannelRequestSuccess is called when a client or server responds to an open channel request
func (src *StandardRicochetConnection) OnOpenChannelRequestSuccess(channelID int32) {
}

// OnChannelClosed is called when a client or server closes an existing channel
func (src *StandardRicochetConnection) OnChannelClosed(channelID int32) {
}

// OnChatMessage is called when a new chat message is received.
func (src *StandardRicochetConnection) OnChatMessage(channelID int32, messageID int32, message string) {
	src.Conn.AckChatMessage(channelID, messageID)
}

// OnChatMessageAck is called when a new chat message is ascknowledged.
func (src *StandardRicochetConnection) OnChatMessageAck(channelID int32, messageID int32) {
}

// OnFailedChannelOpen is called when a server fails to open a channel
func (src *StandardRicochetConnection) OnFailedChannelOpen(channelID int32, errorType string) {
	src.Conn.UnsetChannel(channelID)
}

// OnGenericError is called when a generalized error is returned from the peer
func (src *StandardRicochetConnection) OnGenericError(channelID int32) {
	src.Conn.RejectOpenChannel(channelID, "GenericError")
}

//OnUnknownTypeError is called when an unknown type error is returned from the peer
func (src *StandardRicochetConnection) OnUnknownTypeError(channelID int32) {
	src.Conn.RejectOpenChannel(channelID, "UnknownTypeError")
}

// OnUnauthorizedError is called when an unathorized error is returned from the peer
func (src *StandardRicochetConnection) OnUnauthorizedError(channelID int32) {
	src.Conn.RejectOpenChannel(channelID, "UnauthorizedError")
}

// OnBadUsageError is called when a bad usage error is returned from the peer
func (src *StandardRicochetConnection) OnBadUsageError(channelID int32) {
	src.Conn.RejectOpenChannel(channelID, "BadUsageError")
}

// OnFailedError is called when a failed error is returned from the peer
func (src *StandardRicochetConnection) OnFailedError(channelID int32) {
	src.Conn.RejectOpenChannel(channelID, "FailedError")
}
