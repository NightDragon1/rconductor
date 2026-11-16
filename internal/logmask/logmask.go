// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package logmask masks known secret patterns in container log lines
// before the panel keeps or sends them (UI-022, D4): a built-in list and
// up to MaxPatterns patterns of the RCON admin, all RE2, so a line takes
// time linear in its length, bounded per call by a time limit.
package logmask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/store"
)

// Settings of the masking (UI-022): the switch, on unless set to false,
// the patterns of the RCON admin and the time one call may take.
const (
	SettingEnabled  = "logs.masking"
	SettingPatterns = "logs.mask_patterns"
	SettingTimeout  = "logs.mask_timeout"
)

// Time limit of one call (T-019, SEC-003): default and lowest value.
const (
	DefaultTimeout = time.Second
	MinTimeout     = 100 * time.Millisecond
)

// Bounds of the admin patterns (D4). MaxPatternInsts bounds the program of
// one pattern and MaxTotalInsts that of all together; a line with more
// than MaxAdminMatches matches is withheld as a whole, so the searches of
// one line stay bounded.
const (
	MaxPatterns     = 20
	MaxPatternRunes = 256
	MaxPatternInsts = 1000
	MaxTotalInsts   = 4000
	MaxAdminMatches = 64
)

// Replacement stands for every masked value (D4).
const Replacement = "•••"

// secretKey is a name that marks its value as a secret (D4).
const secretKey = `[A-Za-z0-9_.-]*(?:password|passwd|secret|token|api[_-]?key)[A-Za-z0-9_.-]*` //nolint:gosec // names that mark secrets, no credential

// builtin keeps the name, separator and any opening quote (group 1) and
// masks the value: up to the closing quote, or the line end for an open
// one; a bare value up to space or a separator. Single quotes are the
// form of shell traces (set -x).
var builtin = []*regexp.Regexp{
	// 'KEY=value' as a whole in single quotes: up to the closing quote.
	regexp.MustCompile(`(?i)('` + secretKey + `\s*[=:]\s*)(?:[^']|'\\'')*`),
	// KEY=value, key: value, "key": "value", also with prefixes such as
	// RCON_PASSWORD or db_password.
	regexp.MustCompile(`(?i)(\b` + secretKey + `"?\s*[=:]\s*")[^"]*`),
	regexp.MustCompile(`(?i)(\b` + secretKey + `"?\s*[=:]\s*')[^']*`),
	regexp.MustCompile(`(?i)(\b` + secretKey + `"?\s*[=:]\s*)[^\s"',;&]+`),
	// Command-line flags: --rcon-password value, -password "value",
	// --rcon-password 'value'.
	regexp.MustCompile(`(?i)(-` + secretKey + `\s+")[^"]*`),
	regexp.MustCompile(`(?i)(-` + secretKey + `\s+')(?:[^']|'\\'')*`),
	regexp.MustCompile(`(?i)(-` + secretKey + `\s+)[^\s"']+`),
	// Authorization: Bearer <token>.
	regexp.MustCompile(`(?i)(\bBearer\s+)[A-Za-z0-9._~+/=-]+`),
	// User info of a URL: scheme://user:password@host.
	regexp.MustCompile(`(?i)(\b[a-z][a-z0-9+.-]*://)[^\s/@]+(@)`),
}

// Masker masks lines with the built-in list and the admin patterns.
type Masker struct{ admin *regexp.Regexp }

// Compile checks admin patterns: at most MaxPatterns, each RE2 with 1 to
// MaxPatternRunes characters, a program of at most MaxPatternInsts
// instructions and no match of the empty string; the error names the
// first bad one. The patterns run as one alternation, in one pass.
func Compile(patterns []string) (*Masker, error) {
	if len(patterns) > MaxPatterns {
		return nil, fmt.Errorf("at most %d patterns", MaxPatterns)
	}
	parts := make([]string, 0, len(patterns))
	total := 0
	for i, p := range patterns {
		if p == "" || utf8.RuneCountInString(p) > MaxPatternRunes {
			return nil, fmt.Errorf("pattern %d: 1 to %d characters", i+1, MaxPatternRunes)
		}
		// Checked in the form it runs in: \Q without \E would swallow the
		// closing parenthesis. The error never holds the pattern (SEC-012).
		part := "(?:" + p + ")"
		re, err := regexp.Compile(part)
		if err != nil {
			return nil, fmt.Errorf("pattern %d: not a valid RE2 expression", i+1)
		}
		if re.MatchString("") || emptyMatch(re) {
			return nil, fmt.Errorf("pattern %d: matches empty text", i+1)
		}
		n, err := insts(part)
		if err != nil || n > MaxPatternInsts {
			return nil, fmt.Errorf("pattern %d: too complex", i+1)
		}
		if total += n; total > MaxTotalInsts {
			return nil, fmt.Errorf("patterns together too complex")
		}
		parts = append(parts, part)
	}
	m := &Masker{}
	if len(parts) > 0 {
		re, err := regexp.Compile(strings.Join(parts, "|"))
		if err != nil {
			return nil, fmt.Errorf("patterns do not combine")
		}
		m.admin = re
	}
	return m, nil
}

// emptyMatch tells whether re matches empty text somewhere in a probe,
// such as at a word boundary.
func emptyMatch(re *regexp.Regexp) bool {
	for _, loc := range re.FindAllStringIndex("a b-c_1.\n\tz", -1) {
		if loc[0] == loc[1] {
			return true
		}
	}
	return false
}

// insts is the size of the compiled program of p.
func insts(p string) (int, error) {
	re, err := syntax.Parse(p, syntax.Perl)
	if err != nil {
		return 0, err
	}
	prog, err := syntax.Compile(re.Simplify())
	if err != nil {
		return 0, err
	}
	return len(prog.Inst), nil
}

// Line masks one line: built-in values keep their name, an admin pattern
// is replaced as a whole. The result is cut to the line bound of the
// protocol.
func (m *Masker) Line(s string) string {
	s = cut(s, agentproto.MaxLogLine)
	for _, re := range builtin {
		s = re.ReplaceAllString(s, "${1}"+Replacement+"${2}")
	}
	if m.admin != nil {
		// At most MaxAdminMatches searches: a line with more is withheld.
		locs := m.admin.FindAllStringIndex(s, MaxAdminMatches+1)
		if len(locs) > MaxAdminMatches {
			return Replacement
		}
		var b strings.Builder
		last := 0
		for _, l := range locs {
			b.WriteString(s[last:l[0]])
			b.WriteString(Replacement)
			last = l[1]
		}
		b.WriteString(s[last:])
		s = b.String()
	}
	return cut(s, agentproto.MaxLogLine)
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

// Service masks lines after the current settings; it compiles the admin
// patterns again only when they change.
type Service struct {
	st *store.Store
	// now is the clock of the time limit; tests set their own.
	now func() time.Time

	mu   sync.Mutex
	raw  string
	last *Masker
}

// New returns the masking of the panel.
func New(st *store.Store) *Service { return &Service{st: st, now: time.Now, last: &Masker{}} }

// Lines masks the lines in place and returns them, unless the RCON admin
// switched the masking off. It fails closed: without a readable switch it
// masks, without readable patterns it keeps the last ones, and lines left
// when the time limit or ctx ends become Replacement.
func (s *Service) Lines(ctx context.Context, lines []string) []string {
	if s == nil || len(lines) == 0 {
		return lines
	}
	on := true
	if err := s.st.GlobalSetting(ctx, SettingEnabled, &on); err != nil {
		on = true
	}
	if !on {
		return lines
	}
	timeout := DefaultTimeout
	var v string
	if s.st.GlobalSetting(ctx, SettingTimeout, &v) == nil {
		if d, err := time.ParseDuration(v); err == nil && d >= MinTimeout {
			timeout = d
		}
	}
	m := s.masker(ctx)
	deadline := s.now().Add(timeout)
	for i, l := range lines {
		if ctx.Err() != nil || s.now().After(deadline) {
			lines[i] = Replacement
			continue
		}
		lines[i] = m.Line(l)
	}
	return lines
}

// masker is the masker of the stored patterns; it keeps the last one when
// they cannot be read or do not compile.
func (s *Service) masker(ctx context.Context) *Masker {
	var patterns []string
	if err := s.st.GlobalSetting(ctx, SettingPatterns, &patterns); err != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.last
	}
	b, _ := json.Marshal(patterns)
	raw := string(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	if raw == s.raw {
		return s.last
	}
	m, err := Compile(patterns)
	if err != nil {
		// Stored patterns passed the same check; keep the last masker.
		return s.last
	}
	s.raw, s.last = raw, m
	return m
}

// ErrPatterns marks an invalid list of admin patterns (400, UI-022).
var ErrPatterns = errors.New("invalid mask patterns")

// CheckPatterns is the settings rule of SettingPatterns.
func CheckPatterns(raw json.RawMessage) (any, error) {
	var patterns []string
	if string(raw) == "null" || json.Unmarshal(raw, &patterns) != nil {
		return nil, fmt.Errorf("%w: a list of strings", ErrPatterns)
	}
	if _, err := Compile(patterns); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPatterns, err)
	}
	if patterns == nil {
		patterns = []string{}
	}
	return patterns, nil
}
