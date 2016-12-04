package utils

import (
	"crypto/sha1"
	"encoding/base32"
	"strings"
)

// GetTorHostname takes a []byte contained a DER-encoded RSA public key
// and returns the first 16 bytes of the base32 encoded sha1 hash of the key.
// This is the onion hostname of the tor service represented by the public key.
func GetTorHostname(publicKeyBytes []byte) string {
	h := sha1.New()
	h.Write(publicKeyBytes)
	sha1bytes := h.Sum(nil)

	data := base32.StdEncoding.EncodeToString(sha1bytes)
	return strings.ToLower(data[0:16])
}
