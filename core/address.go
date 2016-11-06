package core

import (
	"crypto/rsa"
	"errors"
	"github.com/yawning/bulb/utils/pkcs1"
	"strings"
)

// Conversion functions between ricochet addresses, onion hostnames, and 80-bit base32 encoded fingerprints.
// As used in this file, these are referred to as 'address', 'onion', and 'plain host' respectively.

func isBase32Valid(str string) bool {
	for _, c := range []byte(str) {
		if (c < 'a' || c > 'z') && (c < '2' || c > '7') {
			return false
		}
	}
	return true
}

func IsAddressValid(addr string) bool {
	return len(addr) == 25 && strings.HasPrefix(addr, "ricochet:") && isBase32Valid(addr[9:])
}

func IsOnionValid(onion string) bool {
	return len(onion) == 22 && strings.HasSuffix(onion, ".onion") && isBase32Valid(onion[0:16])
}

func IsPlainHostValid(host string) bool {
	return len(host) == 16 && isBase32Valid(host)
}

func AddressFromOnion(onion string) (string, bool) {
	if !IsOnionValid(onion) {
		return "", false
	}
	return "ricochet:" + onion[0:16], true
}

func OnionFromAddress(addr string) (string, bool) {
	if !IsAddressValid(addr) {
		return "", false
	}
	return addr[9:] + ".onion", true
}

func OnionFromPlainHost(host string) (string, bool) {
	if !IsPlainHostValid(host) {
		return "", false
	}
	return host + ".onion", true
}

func PlainHostFromOnion(onion string) (string, bool) {
	if !IsOnionValid(onion) {
		return "", false
	}
	return onion[0:16], true
}

func AddressFromPlainHost(host string) (string, bool) {
	if !IsPlainHostValid(host) {
		return "", false
	}
	return "ricochet:" + host, true
}

func PlainHostFromAddress(host string) (string, bool) {
	if !IsAddressValid(host) {
		return "", false
	}
	return host[9:], true
}

func AddressFromKey(key *rsa.PublicKey) (string, error) {
	addr, err := pkcs1.OnionAddr(key)
	if err != nil {
		return "", err
	} else if addr == "" {
		return "", errors.New("Invalid key")
	}
	return "ricochet:" + addr, nil
}
