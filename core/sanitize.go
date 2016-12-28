package core

import (
	"unicode"
	"unicode/utf8"
)

const (
	// Consistent with protocol's ContactRequestChannel
	MaxMessageLength  = 2000
	MaxNicknameLength = 30
)

// IsNicknameAcceptable returns true for strings that are usable as contact nicknames.
// A nickname is acceptable if it:
//   - Is composed of only valid UTF-8 sequences
//   - Has between 1 and MaxNicknameLength unicode characters
//   - Doesn't contain any characters from unicode Cf or Cc
//   - Doesn't contain any of these HTML-sensitive characters: "<>&\
//   - XXX This could use more thought on valid codepoints; note that Go
//     has a good set of built-in range tables in `unicode`
func IsNicknameAcceptable(nickname string) bool {
	length := 0

	blacklist := []rune{'"', '<', '>', '&', '\\'}

	for len(nickname) > 0 {
		r, sz := utf8.DecodeRuneInString(nickname)
		if r == utf8.RuneError {
			return false
		}

		if unicode.In(r, unicode.Cf, unicode.Cc) {
			return false
		}

		for _, br := range blacklist {
			if r == br {
				return false
			}
		}

		length++
		if length > MaxNicknameLength {
			return false
		}
		nickname = nickname[sz:]
	}

	return length > 0
}

// IsMessageAcceptable returns true for strings that are usable as chat messages.
// A message is acceptable if it:
//   - Is composed of only valid UTF-8 sequences
//   - Encodes to between 1 and MaxMessageLength bytes in UTF-8
//   - XXX This also needs more thought on valid unicode characters
func IsMessageAcceptable(message string) bool {
	return len(message) > 0 &&
		len(message) <= MaxMessageLength &&
		utf8.ValidString(message)
}
