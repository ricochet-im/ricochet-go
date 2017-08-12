package application

import (
	"crypto/rsa"
)

// AcceptAllContactManager implements the contact manager interface an presumes
// all connections are allowed.
type AcceptAllContactManager struct {
}

// LookupContact returns that a contact is known and allowed to communicate for all cases.
func (aacm *AcceptAllContactManager) LookupContact(hostname string, publicKey rsa.PublicKey) (allowed, known bool) {
	return true, true
}

func (aacm *AcceptAllContactManager) ContactRequest(name string, message string) string {
	return "Accepted"
}
