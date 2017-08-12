package application

import (
	"crypto/rsa"
)

// ContactManagerInterface provides a mechanism for autonous applications
// to make decisions on what connections to accept or reject.
type ContactManagerInterface interface {
	LookupContact(hostname string, publicKey rsa.PublicKey) (allowed, known bool)
}
