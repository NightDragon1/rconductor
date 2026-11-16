// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package logmask masks known secrets in container log lines before the
// panel keeps or sends them (UI-022, D-088). Sources in this order: a
// built-in list of RE2 expressions, fixed in the code; then the fixed texts
// of the RCON admin, no expressions. Each source takes time linear in the
// length of the line, so the cost of a line is bounded by the number of
// sources and texts.
package logmask

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/NightDragon1/rconductor/internal/agentproto"
)

// Replacement stands for every masked value (D-088).
const Replacement = "•••"

// Bounds of the fixed texts of the RCON admin (D-088): a text has at least
// MinTextRunes characters, so that common words are no texts; the length
// and the number are settings with these defaults and upper ends.
const (
	MinTextRunes     = 4
	DefaultTextRunes = 256
	MaxTextRunes     = 1024
	DefaultTexts     = 50
	MaxTexts         = 200
)

// secretKey is a name that marks its value as a secret.
const secretKey = `[A-Za-z0-9_.-]*(?:password|passwd|secret|token|api[_-]?key)[A-Za-z0-9_.-]*` //nolint:gosec // names that mark secrets, no credential

// quoted is the inside of single quotes in a shell trace (set -x), where a
// quote in the value is closed, escaped and opened again.
const quoted = `(?:[^']|'\\'')*`

// builtin keeps the name, separator and any opening quote (group 1) and
// masks the value: up to the closing quote, or the line end for an open
// one; a bare value up to space or a separator.
var builtin = []*regexp.Regexp{
	// 'KEY=value' as a whole in single quotes: up to the closing quote.
	regexp.MustCompile(`(?i)('` + secretKey + `\s*[=:]\s*)` + quoted),
	// KEY=value, key: value, "key": "value", also with prefixes such as
	// RCON_PASSWORD or db_password.
	regexp.MustCompile(`(?i)(\b` + secretKey + `"?\s*[=:]\s*")[^"]*`),
	regexp.MustCompile(`(?i)(\b` + secretKey + `"?\s*[=:]\s*')` + quoted),
	regexp.MustCompile(`(?i)(\b` + secretKey + `"?\s*[=:]\s*)[^\s"',;&]+`),
	// Command-line flags: --rcon-password value, -password "value",
	// --rcon-password 'value'.
	regexp.MustCompile(`(?i)(-` + secretKey + `\s+")[^"]*`),
	regexp.MustCompile(`(?i)(-` + secretKey + `\s+')` + quoted),
	regexp.MustCompile(`(?i)(-` + secretKey + `\s+)[^\s"']+`),
	// Authorization: Bearer <token>.
	regexp.MustCompile(`(?i)(\bBearer\s+)[A-Za-z0-9._~+/=-]+`),
	// User info of a URL: scheme://user:password@host.
	regexp.MustCompile(`(?i)(\b[a-z][a-z0-9+.-]*://)[^\s/@]+(@)`),
}

// Text is a fixed text of the RCON admin; Fold ignores the case of the
// letters A to Z, so that the masked part keeps its length in bytes.
type Text struct {
	Value string
	Fold  bool
}

// Limits are the bounds of the texts from the settings.
type Limits struct {
	Texts     int // at most this many texts, 1 to MaxTexts
	TextRunes int // each with at most this many characters, MinTextRunes to MaxTextRunes
}

// DefaultLimits are the limits without settings.
var DefaultLimits = Limits{Texts: DefaultTexts, TextRunes: DefaultTextRunes}

// Masker masks lines with the built-in list and the texts of the admin.
type Masker struct {
	exact, fold []needle // fold holds the texts in lower case
}

// needle is a text with its KMP table, so that one pass over a line finds
// every place of the text, also overlapping ones, in linear time.
type needle struct {
	text string
	next []int
}

func newNeedle(t string) needle {
	next := make([]int, len(t))
	for i, k := 1, 0; i < len(t); i++ {
		for k > 0 && t[i] != t[k] {
			k = next[k-1]
		}
		if t[i] == t[k] {
			k++
		}
		next[i] = k
	}
	return needle{text: t, next: next}
}

// mark counts every place of the text in cover, as +1 at its start and
// -1 after its end, so that dense overlapping places cost one step each.
func (n needle) mark(in string, cover []int32) bool {
	found := false
	for i, k := 0, 0; i < len(in); i++ {
		for k > 0 && in[i] != n.text[k] {
			k = n.next[k-1]
		}
		if in[i] == n.text[k] {
			k++
		}
		if k == len(n.text) {
			cover[i+1-k]++
			cover[i+1]--
			found = true
			k = n.next[k-1]
		}
	}
	return found
}

// New checks the texts against lim and returns their masker. The error
// names the first bad text by its position, never by its content (SEC-012).
func New(texts []Text, lim Limits) (*Masker, error) {
	lim.Texts = min(max(lim.Texts, 1), MaxTexts)
	lim.TextRunes = min(max(lim.TextRunes, MinTextRunes), MaxTextRunes)
	if len(texts) > lim.Texts {
		return nil, fmt.Errorf("at most %d texts", lim.Texts)
	}
	m := &Masker{}
	for i, t := range texts {
		n := utf8.RuneCountInString(t.Value)
		if !utf8.ValidString(t.Value) || n < MinTextRunes || n > lim.TextRunes || strings.ContainsAny(t.Value, "\r\n") {
			return nil, fmt.Errorf("text %d: %d to %d characters on one line", i+1, MinTextRunes, lim.TextRunes)
		}
		if t.Fold {
			m.fold = append(m.fold, newNeedle(lowerASCII(t.Value)))
		} else {
			m.exact = append(m.exact, newNeedle(t.Value))
		}
	}
	return m, nil
}

// Line masks one line: built-in values keep their name, a text of the
// admin is replaced as a whole. The result is cut to the line bound of the
// protocol.
func (m *Masker) Line(s string) string {
	s = cut(s, agentproto.MaxLogLine)
	for _, re := range builtin {
		s = re.ReplaceAllString(s, "${1}"+Replacement+"${2}")
	}
	if m != nil && len(m.exact)+len(m.fold) > 0 {
		s = m.texts(s)
	}
	return cut(s, agentproto.MaxLogLine)
}

// texts finds every place of every text and replaces each run of covered
// bytes once, so that overlapping texts mask their union.
func (m *Masker) texts(s string) string {
	cover := make([]int32, len(s)+1)
	found := false
	for _, n := range m.exact {
		found = n.mark(s, cover) || found
	}
	if len(m.fold) > 0 {
		lower := lowerASCII(s)
		for _, n := range m.fold {
			found = n.mark(lower, cover) || found
		}
	}
	if !found {
		return s
	}
	var b strings.Builder
	depth, hidden := int32(0), false
	for i := 0; i < len(s); i++ {
		depth += cover[i]
		switch {
		case depth == 0:
			b.WriteByte(s[i])
			hidden = false
		case !hidden:
			b.WriteString(Replacement)
			hidden = true
		}
	}
	return b.String()
}

// lowerASCII lowers A to Z only, so that byte positions stay the same.
func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// cut bounds s to n bytes on a rune boundary.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
