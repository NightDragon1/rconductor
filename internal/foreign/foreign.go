// SPDX-License-Identifier: AGPL-3.0-or-later

// Package foreign cleans data from game servers and agents before the
// panel keeps or forwards it (SEC-006): valid UTF-8, bounded in size,
// without control characters. Escaping is left to the output (JSON here,
// the interface in the browser); color codes stay for the interface to
// translate into its fixed list of styles.
package foreign

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Text cleans free text such as an RCON answer or a log line: invalid
// UTF-8 becomes U+FFFD, line ends become \n, and control characters other
// than \n and \t are dropped. Format characters stay, longer text is the
// interface's to isolate (revision 2.2 candidate 9). The result has at most
// maxBytes bytes, cut at a character boundary; truncated reports a cut.
func Text(s string, maxBytes int) (out string, truncated bool) {
	s = strings.ToValidUTF8(s, "\uFFFD")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	b.Grow(min(len(s), maxBytes))
	for _, r := range s {
		switch {
		case r == '\r':
			r = '\n'
		case r != '\n' && r != '\t' && unicode.IsControl(r):
			continue
		}
		if b.Len()+utf8.RuneLen(r) > maxBytes {
			return b.String(), true
		}
		b.WriteRune(r)
	}
	return b.String(), false
}

// Name cleans an identifier such as a player or container name: control
// and format characters (among them bidi overrides that reverse the
// display) are dropped, surrounding space is trimmed and at most maxRunes
// characters are kept (revision 2.2 candidate 9).
func Name(s string, maxRunes int) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s))
	if utf8.RuneCountInString(s) > maxRunes {
		s = strings.TrimSpace(string([]rune(s)[:maxRunes]))
	}
	return s
}
