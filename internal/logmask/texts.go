// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package logmask

import (
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/logging"
)

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

// Text is a fixed text of the RCON admin; Fold ignores the case of the
// letters A to Z, so that the masked part keeps its length in bytes.
type Text struct {
	Value string
	Fold  bool
}

// LogValue keeps a text out of logs (SEC-012).
func (t Text) LogValue() slog.Value { return slog.StringValue(logging.Redacted) }

// MarshalJSON keeps a text out of JSON output, such as a JSON log of a
// slice; the settings seal texts through their own type.
func (t Text) MarshalJSON() ([]byte, error) { return []byte(`"` + logging.Redacted + `"`), nil }

// Format keeps a text out of fmt output, with every verb.
func (t Text) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, logging.Redacted) }

// LogValue keeps the texts of a masker out of logs (SEC-012).
func (m Masker) LogValue() slog.Value { return slog.StringValue(logging.Redacted) }

// Format keeps the texts of a masker out of fmt output.
func (m Masker) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, logging.Redacted) }

// Limits are the bounds of the texts from the settings.
type Limits struct {
	Texts     int // at most this many texts, 1 to MaxTexts
	TextRunes int // each with at most this many characters, MinTextRunes to MaxTextRunes
}

// DefaultLimits are the limits without settings.
var DefaultLimits = Limits{Texts: DefaultTexts, TextRunes: DefaultTextRunes}

// Masker masks lines with the built-in list, the patterns of the
// profiles of a server and the texts of the admin; it does not change
// after it is built.
type Masker struct {
	patterns    []*regexp.Regexp
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
// On a line cut at the bound, a start of the text at its end counts too.
func (n needle) mark(in string, cover []int32, atBound bool) bool {
	found, k := false, 0
	for i := 0; i < len(in); i++ {
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
	if atBound && k > 0 {
		cover[len(in)-k]++
		cover[len(in)]--
		found = true
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

// markTexts counts every place of every text in cover.
func (m *Masker) markTexts(s string, cover []int32) bool {
	found := false
	atBound := len(s) > agentproto.MaxLogLine-utf8.UTFMax
	for _, n := range m.exact {
		found = n.mark(s, cover, atBound) || found
	}
	if len(m.fold) > 0 {
		lower := lowerASCII(s)
		for _, n := range m.fold {
			found = n.mark(lower, cover, atBound) || found
		}
	}
	return found
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
