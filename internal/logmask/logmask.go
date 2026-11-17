// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package logmask masks known secrets in container log lines before the
// panel keeps or sends them (UI-022, D-088). Every source takes time
// linear in the length of the line; this part holds the built-in list of
// RE2 expressions, fixed in the code.
package logmask

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/NightDragon1/rconductor/internal/agentproto"
)

// Replacement stands for every masked value (D-088).
const Replacement = "•••"

// secretKey is a name that marks its value as a secret.
const secretKey = `[A-Za-z0-9_.-]*(?:password|passwd|secret|token|api[_-]?key)[A-Za-z0-9_.-]*` //nolint:gosec // names that mark secrets, no credential

// quoted is the inside of single quotes in a shell trace (set -x), where a
// quote in the value is closed, escaped and opened again.
const quoted = `(?:[^']|'\\'')*`

// dquoted is the inside of double quotes, where \" is a quote of the value.
const dquoted = `(?:[^"\\]|\\.)*`

// builtin keeps the name, separator and any opening quote (group 1) and
// any group 2, and masks the rest of the match: up to the closing quote, or
// the line end for an open one; a bare value up to space or a separator.
var builtin = []*regexp.Regexp{
	// 'KEY=value' as a whole in single quotes: up to the closing quote.
	regexp.MustCompile(`(?i)('` + secretKey + `\s*[=:]\s*)` + quoted),
	// KEY=value, key: value, "key": "value", also with prefixes such as
	// RCON_PASSWORD or db_password.
	regexp.MustCompile(`(?i)(\b` + secretKey + `"?\s*[=:]\s*")` + dquoted),
	regexp.MustCompile(`(?i)(\b` + secretKey + `"?\s*[=:]\s*')` + quoted),
	regexp.MustCompile(`(?i)(\b` + secretKey + `"?\s*[=:]\s*)[^\s"',;&]+`),
	// Command-line flags: --rcon-password value, -password "value",
	// --rcon-password 'value'.
	regexp.MustCompile(`(?i)(-` + secretKey + `\s+")` + dquoted),
	regexp.MustCompile(`(?i)(-` + secretKey + `\s+')` + quoted),
	regexp.MustCompile(`(?i)(-` + secretKey + `\s+)[^\s"']+`),
	// Authorization: Bearer <token>.
	regexp.MustCompile(`(?i)(\bBearer\s+)[A-Za-z0-9._~+/=-]+`),
	// User info of a URL: scheme://user:password@host.
	regexp.MustCompile(`(?i)(\b[a-z][a-z0-9+.-]*://)[^\s/@]+(@)`),
}

// Masker masks lines; it holds what the settings add to the built-in list.
type Masker struct{}

// Line masks one line: built-in values keep their name. Every source
// searches the line as it came, into one cover, and each run of covered
// bytes becomes one Replacement, so no source misses what another one
// changed. The result is cut to the line bound of the protocol.
func (m *Masker) Line(s string) string {
	s = cut(s, agentproto.MaxLogLine)
	cover := make([]int32, len(s)+1)
	found := false
	for _, re := range builtin {
		for _, l := range re.FindAllStringSubmatchIndex(s, -1) {
			from, to := l[3], l[1]
			if len(l) > 4 && l[4] >= 0 {
				to = l[4]
			}
			if from < to {
				cover[from]++
				cover[to]--
				found = true
			}
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
	return cut(b.String(), agentproto.MaxLogLine)
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
