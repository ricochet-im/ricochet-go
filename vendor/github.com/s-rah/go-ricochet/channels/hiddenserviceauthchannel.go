package channels

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/asn1"
	"github.com/golang/protobuf/proto"
	"github.com/s-rah/go-ricochet/utils"
	"github.com/s-rah/go-ricochet/wire/auth"
	"github.com/s-rah/go-ricochet/wire/control"
	"io"
)

const (
	InvalidClientCookieError = utils.Error("InvalidClientCookieError")
)

// HiddenServiceAuthChannel wraps implementation of im.ricochet.auth.hidden-service"
type HiddenServiceAuthChannel struct {
	// PrivateKey must be set for client-side authentication channels
	PrivateKey *rsa.PrivateKey
	// Server Hostname must be set for client-side authentication channels
	ServerHostname string

	// Callbacks
	ClientAuthResult  func(accepted, isKnownContact bool)
	ServerAuthValid   func(hostname string, publicKey rsa.PublicKey) (allowed, known bool)
	ServerAuthInvalid func(err error)

	// Internal state
	clientCookie, serverCookie [16]byte
	channel                    *Channel
}

// Type returns the type string for this channel, e.g. "im.ricochet.chat".
func (ah *HiddenServiceAuthChannel) Type() string {
	return "im.ricochet.auth.hidden-service"
}

// Singleton Returns whether or not the given channel type is a singleton
func (ah *HiddenServiceAuthChannel) Singleton() bool {
	return true
}

// OnlyClientCanOpen ...
func (ah *HiddenServiceAuthChannel) OnlyClientCanOpen() bool {
	return true
}

// Bidirectional Returns whether or not the given channel allows anyone to send messages
func (ah *HiddenServiceAuthChannel) Bidirectional() bool {
	return false
}

// RequiresAuthentication Returns whether or not the given channel type requires authentication
func (ah *HiddenServiceAuthChannel) RequiresAuthentication() string {
	return "none"
}

// Closed is called when the channel is closed for any reason.
func (ah *HiddenServiceAuthChannel) Closed(err error) {

}

// OpenInbound is the first method called for an inbound channel request.
// If an error is returned, the channel is rejected. If a RawMessage is
// returned, it will be sent as the ChannelResult message.
// Remote -> [Open Authentication Channel] -> Local
func (ah *HiddenServiceAuthChannel) OpenInbound(channel *Channel, oc *Protocol_Data_Control.OpenChannel) ([]byte, error) {

	if ah.PrivateKey == nil {
		return nil, utils.PrivateKeyNotSetError
	}

	ah.channel = channel
	clientCookie, _ := proto.GetExtension(oc, Protocol_Data_AuthHiddenService.E_ClientCookie)
	if len(clientCookie.([]byte)[:]) != 16 {
		// reutrn without opening channel.
		return nil, InvalidClientCookieError
	}
	ah.AddClientCookie(clientCookie.([]byte)[:])
	messageBuilder := new(utils.MessageBuilder)
	channel.Pending = false
	return messageBuilder.ConfirmAuthChannel(ah.channel.ID, ah.GenServerCookie()), nil
}

// OpenOutbound is the first method called for an outbound channel request.
// If an error is returned, the channel is not opened. If a RawMessage is
// returned, it will be sent as the OpenChannel message.
// Local -> [Open Authentication Channel] -> Remote
func (ah *HiddenServiceAuthChannel) OpenOutbound(channel *Channel) ([]byte, error) {

	if ah.PrivateKey == nil {
		return nil, utils.PrivateKeyNotSetError
	}

	ah.channel = channel
	messageBuilder := new(utils.MessageBuilder)
	return messageBuilder.OpenAuthenticationChannel(ah.channel.ID, ah.GenClientCookie()), nil
}

// OpenOutboundResult is called when a response is received for an
// outbound OpenChannel request. If `err` is non-nil, the channel was
// rejected and Closed will be called immediately afterwards. `raw`
// contains the raw protocol message including any extension data.
// Input: Remote -> [ChannelResult] -> {Client}
// Output: {Client} -> [Proof] -> Remote
func (ah *HiddenServiceAuthChannel) OpenOutboundResult(err error, crm *Protocol_Data_Control.ChannelResult) {

	if err == nil {

		if crm.GetOpened() {
			serverCookie, _ := proto.GetExtension(crm, Protocol_Data_AuthHiddenService.E_ServerCookie)

			if len(serverCookie.([]byte)[:]) != 16 {
				ah.channel.SendMessage([]byte{})
				return
			}

			ah.AddServerCookie(serverCookie.([]byte)[:])

			publicKeyBytes, _ := asn1.Marshal(rsa.PublicKey{
				N: ah.PrivateKey.PublicKey.N,
				E: ah.PrivateKey.PublicKey.E,
			})

			clientHostname := utils.GetTorHostname(publicKeyBytes)
			challenge := ah.GenChallenge(clientHostname, ah.ServerHostname)

			signature, err := rsa.SignPKCS1v15(nil, ah.PrivateKey, crypto.SHA256, challenge)

			if err != nil {
				ah.channel.SendMessage([]byte{})
				return
			}

			messageBuilder := new(utils.MessageBuilder)
			proof := messageBuilder.Proof(publicKeyBytes, signature)
			ah.channel.SendMessage(proof)
		}
	}
}

// Packet is called for each raw packet received on this channel.
// Input: Remote -> [Proof] -> Client
// OR
// Input: Remote -> [Result] -> Client
func (ah *HiddenServiceAuthChannel) Packet(data []byte) {
	res := new(Protocol_Data_AuthHiddenService.Packet)
	err := proto.Unmarshal(data[:], res)

	if err != nil {
		ah.channel.CloseChannel()
		return
	}

	if res.GetProof() != nil && ah.channel.Direction == Inbound {
		provisionalClientHostname := utils.GetTorHostname(res.GetProof().GetPublicKey())

		publicKeyBytes, err := asn1.Marshal(rsa.PublicKey{
			N: ah.PrivateKey.PublicKey.N,
			E: ah.PrivateKey.PublicKey.E,
		})

		if err != nil {
			ah.ServerAuthInvalid(err)
			ah.channel.SendMessage([]byte{})
			return
		}

		serverHostname := utils.GetTorHostname(publicKeyBytes)

		publicKey := rsa.PublicKey{}
		_, err = asn1.Unmarshal(res.GetProof().GetPublicKey(), &publicKey)
		if err != nil {
			ah.ServerAuthInvalid(err)
			ah.channel.SendMessage([]byte{})
			return
		}

		challenge := ah.GenChallenge(provisionalClientHostname, serverHostname)

		err = rsa.VerifyPKCS1v15(&publicKey, crypto.SHA256, challenge[:], res.GetProof().GetSignature())

		if err == nil {
			// Signature is Good
			accepted, isKnownContact := ah.ServerAuthValid(provisionalClientHostname, publicKey)

			// Send Result
			messageBuilder := new(utils.MessageBuilder)
			result := messageBuilder.AuthResult(accepted, isKnownContact)
			ah.channel.DelegateAuthorization()
			ah.channel.SendMessage(result)
		} else {
			// Auth Failed
			messageBuilder := new(utils.MessageBuilder)
			result := messageBuilder.AuthResult(false, false)
			ah.channel.SendMessage(result)
			ah.ServerAuthInvalid(err)
		}

	} else if res.GetResult() != nil && ah.channel.Direction == Outbound {
		if ah.ClientAuthResult != nil {
			ah.ClientAuthResult(res.GetResult().GetAccepted(), res.GetResult().GetIsKnownContact())
		}
		if res.GetResult().GetAccepted() {
			ah.channel.DelegateAuthorization()
		}
	}

	// Any other combination of packets is completely invalid
	// Fail the Authorization right here.
	ah.channel.CloseChannel()
}

// AddClientCookie adds a client cookie to the state.
func (ah *HiddenServiceAuthChannel) AddClientCookie(cookie []byte) {
	copy(ah.clientCookie[:], cookie[:16])
}

// AddServerCookie adds a server cookie to the state.
func (ah *HiddenServiceAuthChannel) AddServerCookie(cookie []byte) {
	copy(ah.serverCookie[:], cookie[:16])
}

// GenRandom generates a random 16byte cookie string.
func (ah *HiddenServiceAuthChannel) GenRandom() [16]byte {
	var cookie [16]byte
	io.ReadFull(rand.Reader, cookie[:])
	return cookie
}

// GenClientCookie generates and adds a client cookie to the state.
func (ah *HiddenServiceAuthChannel) GenClientCookie() [16]byte {
	ah.clientCookie = ah.GenRandom()
	return ah.clientCookie
}

// GenServerCookie generates and adds a server cookie to the state.
func (ah *HiddenServiceAuthChannel) GenServerCookie() [16]byte {
	ah.serverCookie = ah.GenRandom()
	return ah.serverCookie
}

// GenChallenge constructs the challenge parameter for the AuthHiddenService session.
// The challenge is the a Sha256HMAC(clientHostname+serverHostname, key=clientCookie+serverCookie)
func (ah *HiddenServiceAuthChannel) GenChallenge(clientHostname string, serverHostname string) []byte {
	key := make([]byte, 32)
	copy(key[0:16], ah.clientCookie[:])
	copy(key[16:], ah.serverCookie[:])

	value := []byte(clientHostname + serverHostname)
	mac := hmac.New(sha256.New, key)
	mac.Write(value)
	hmac := mac.Sum(nil)
	return hmac
}
