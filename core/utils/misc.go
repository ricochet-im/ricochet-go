package utils

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
