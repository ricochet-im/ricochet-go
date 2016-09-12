package utils

import (
	"crypto/rsa"
	"errors"
	"github.com/yawning/bulb/utils/pkcs1"
	"strings"
)

// Take a string containing substrings separated by sep, and
// return a slice of substrings as in strings.Split. Double quotes
// are stripped from the output, and separator characters within
// double quotes are included in the substring verbatim. Quotes and
// escape characters can be escaped with a preceding backslash, which
// is stripped from the output.
func UnquoteStringSplit(s string, sep rune) []string {
	var re []string
	var quoted bool
	var escaped bool
	var current string

	for _, c := range s {
		if c == '"' && !escaped {
			quoted = !quoted
		} else if c == '\\' && !escaped {
			escaped = true
		} else if c == sep && !quoted && !escaped {
			re = append(re, current)
			current = ""
		} else {
			current += string(c)
			escaped = false
		}
	}

	return append(re, current)
}

func RicochetAddressFromKey(key *rsa.PublicKey) (string, error) {
	addr, err := pkcs1.OnionAddr(key)
	if err != nil {
		return "", err
	} else if addr == "" {
		return "", errors.New("Invalid key")
	}
	return "ricochet:" + addr, nil
}

func RicochetAddressFromOnion(onion string) (string, error) {
	if len(onion) != 23 || !strings.HasSuffix(onion, ".onion") {
		return "", errors.New("Invalid onion address")
	}
	return "ricochet:" + onion[:16], nil
}

func OnionFromRicochetAddress(address string) (string, error) {
	if len(address) != 25 || !strings.HasPrefix(address, "ricochet:") {
		return "", errors.New("Invalid ricochet address")
	}
	return address[9:] + ".onion", nil
}
