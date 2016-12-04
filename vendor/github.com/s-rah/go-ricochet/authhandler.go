package goricochet

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"io"
)

// AuthenticationHandler manages the state required for the AuthHiddenService
// authentication scheme for ricochet.
type AuthenticationHandler struct {
	clientCookie [16]byte
	serverCookie [16]byte
}

// AddClientCookie adds a client cookie to the state.
func (ah *AuthenticationHandler) AddClientCookie(cookie []byte) {
	copy(ah.clientCookie[:], cookie[:16])
}

// AddServerCookie adds a server cookie to the state.
func (ah *AuthenticationHandler) AddServerCookie(cookie []byte) {
	copy(ah.serverCookie[:], cookie[:16])
}

// GenRandom generates a random 16byte cookie string.
func (ah *AuthenticationHandler) GenRandom() [16]byte {
	var cookie [16]byte
	io.ReadFull(rand.Reader, cookie[:])
	return cookie
}

// GenClientCookie generates and adds a client cookie to the state.
func (ah *AuthenticationHandler) GenClientCookie() [16]byte {
	ah.clientCookie = ah.GenRandom()
	return ah.clientCookie
}

// GenServerCookie generates and adds a server cookie to the state.
func (ah *AuthenticationHandler) GenServerCookie() [16]byte {
	ah.serverCookie = ah.GenRandom()
	return ah.serverCookie
}

// GenChallenge constructs the challenge parameter for the AuthHiddenService session.
// The challenge is the a Sha256HMAC(clientHostname+serverHostname, key=clientCookie+serverCookie)
func (ah *AuthenticationHandler) GenChallenge(clientHostname string, serverHostname string) []byte {
	key := make([]byte, 32)
	copy(key[0:16], ah.clientCookie[:])
	copy(key[16:], ah.serverCookie[:])
	value := []byte(clientHostname + serverHostname)
	mac := hmac.New(sha256.New, key)
	mac.Write(value)
	hmac := mac.Sum(nil)
	return hmac
}
