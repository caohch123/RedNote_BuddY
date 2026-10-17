package app

import "unicode/utf8"

// Keep error text UTF-8 when presentation truncates it.
func trimMessage(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
