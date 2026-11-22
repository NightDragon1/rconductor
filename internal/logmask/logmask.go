// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package logmask masks known secrets in container log lines before the
// panel keeps or sends them (UI-022, D-088). Sources in this order: a
// built-in list of RE2 expressions, fixed in the code; the RE2 patterns of
// the profiles of the server; then the fixed texts of the RCON admin, no
// expressions. The list and the texts take time linear in the length of
// the line; a profile pattern can take more, so Lines bounds a batch in
// time, also between the patterns of one line.
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

// gap is space and terminal escape sequences, as a colored log puts them
// around a name or a separator: CSI with any parameters, also colon ones,
// and the choice of a character set such as ESC ( B.
const gap = `(?:\s|\x1b(?:\[[0-?]*[ -/]*[@-~]|[()]\w))*`

// sep is the end of a name and the separator: a closing quote of the name,
// also an escaped one or one of a Python dict, then =, ==, : or :=, with
// a gap around it.
const sep = `(?:\\?["'])?` + gap + `(?::=|==?|:)` + gap

// builtin keeps the name, separator and any opening quote (group 1) and
// any group 2, and masks the rest of the match: up to the closing quote, or
// the line end for an open one; a bare value up to space or a separator.
var builtin = []*regexp.Regexp{
	// 'KEY=value' as a whole in single quotes: up to the closing quote.
	regexp.MustCompile(`(?i)('` + secretKey + gap + `(?::=|[=:])\s*)` + quoted),
	// KEY=value, key: value, "key": "value", 'key': 'value', key := value,
	// also with prefixes such as RCON_PASSWORD or db_password.
	regexp.MustCompile(`(?i)(\b` + secretKey + sep + `")` + dquoted),
	regexp.MustCompile(`(?i)(\b` + secretKey + sep + `')` + quoted),
	// Escaped JSON inside a string: \"key\":\"value\", where \\\" is a
	// quote of the value.
	regexp.MustCompile(`(?i)(\b` + secretKey + sep + `\\")(?:[^\\]|\\\\\\"|\\[^"])*`),
	// A bare value; a backslash before a quote opens an escaped value, and
	// an = before a quote belongs to the separator.
	regexp.MustCompile(`(?i)(\b` + secretKey + sep + `)(?:=?[^\s"',;&\\=]|\\[^"']|\\$)(?:[^\s"',;&\\]|\\[^"']|\\$)*`),
	// Command-line flags: --rcon-password value, -password "value",
	// --rcon-password 'value'.
	regexp.MustCompile(`(?i)(-` + secretKey + `\s+")` + dquoted),
	regexp.MustCompile(`(?i)(-` + secretKey + `\s+')` + quoted),
	regexp.MustCompile(`(?i)(-` + secretKey + `\s+)[^\s"']+`),
	// Authorization: Bearer <token>.
	regexp.MustCompile(`(?i)(\bBearer\s+)[A-Za-z0-9._~+/=-]+`),
	// User info of a URL: scheme://user:password@host, up to the first @
	// and, for an @ in the password, up to the last one before the path.
	regexp.MustCompile(`(?i)(\b[a-z][a-z0-9+.-]*://)[^\s/@]+(@)`),
	regexp.MustCompile(`(?i)(\b[a-z][a-z0-9+.-]*://)[^\s/?#]+(@)`),
	// DSN of a Go SQL driver: user:password@tcp(host)/db, user:password@/db.
	regexp.MustCompile(`(?i)(\b[A-Za-z0-9_.-]+:)[^\s]+?(@(?:(?:tcp6?|udp|unix)\(|/))`),
}

// Line masks one line: built-in values keep their name, a text of the
// admin is replaced as a whole. Every source
// searches the line as it came, into one cover, and each run of covered
// bytes becomes one Replacement, so no source misses what another one
// changed. The result is cut to the line bound of the protocol.
func (m *Masker) Line(s string) string {
	out, _ := m.line(s, nil)
	return out
}

// line is Line; over, if set, is asked between the sources and patterns,
// and once it reports true the line is not finished: stopped is true and
// the line must not leave.
func (m *Masker) line(s string, over func() bool) (out string, stopped bool) {
	s = cut(s, agentproto.MaxLogLine)
	cover := make([]int32, len(s)+1)
	found, stopped := markPatterns(builtin, s, cover, over)
	if m != nil && !stopped {
		var more bool
		more, stopped = markPatterns(m.patterns, s, cover, over)
		found = more || found
		if !stopped {
			found = m.markTexts(s, cover) || found
		}
	}
	if stopped {
		return "", true
	}
	if !found {
		return s, false
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
	return cut(b.String(), agentproto.MaxLogLine), false
}

// markPatterns counts every match of the patterns in cover: from the end
// of group 1 to the start of group 2 or the end of the match. A match the
// profile check rules out masks as a whole: without group 1, with text
// before it, or with nothing between group 1 and a group 2 that follows;
// an empty value after group 1 masks nothing, so its name stays.
// over is asked before each pattern; stopped reports that it said true.
func markPatterns(res []*regexp.Regexp, s string, cover []int32, over func() bool) (found, stopped bool) {
	for _, re := range res {
		if over != nil && over() {
			return found, true
		}
		for _, l := range re.FindAllStringSubmatchIndex(s, -1) {
			from, to := l[0], l[1]
			if len(l) > 3 && l[2] == l[0] && l[3] >= 0 {
				from = l[3]
			}
			if len(l) > 5 && l[4] >= from {
				to = l[4]
			}
			if from >= to && l[1] > from {
				// Something after group 1 but nothing to mask: whole match.
				from, to = l[0], l[1]
			}
			if from < to {
				cover[from]++
				cover[to]--
				found = true
			}
		}
	}
	return found, false
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
