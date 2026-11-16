// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package logmask masks known secret patterns in container log lines
// before the panel keeps or sends them (UI-022, D4): a built-in list and
// up to MaxPatterns patterns of the RCON admin, all RE2, so a line takes
// time linear in its length.
package logmask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"unicode/utf8"

	"github.com/NightDragon1/rconductor/internal/store"
)

// Settings of the masking (UI-022): the switch, on unless set to false,
// and the patterns of the RCON admin.
const (
	SettingEnabled  = "logs.masking"
	SettingPatterns = "logs.mask_patterns"
)

// Bounds of the admin patterns (D4).
const (
	MaxPatterns     = 20
	MaxPatternRunes = 256
)

// Replacement stands for every masked value (D4).
const Replacement = "•••"

// secretKey is a name that marks its value as a secret (D4).
const secretKey = `[A-Za-z0-9_.-]*(?:password|passwd|secret|token|api[_-]?key)[A-Za-z0-9_.-]*` //nolint:gosec // names that mark secrets, no credential

// builtin keeps the name, separator and any opening quote (group 1) and
// masks the value: up to the closing quote, or the line end for an open
// one; a bare value up to space or a separator.
var builtin = []*regexp.Regexp{
	// KEY=value, key: value, "key": "value", also with prefixes such as
	// RCON_PASSWORD or db_password.
	regexp.MustCompile(`(?i)(\b` + secretKey + `"?\s*[=:]\s*")[^"]*`),
	regexp.MustCompile(`(?i)(\b` + secretKey + `"?\s*[=:]\s*')[^']*`),
	regexp.MustCompile(`(?i)(\b` + secretKey + `"?\s*[=:]\s*)[^\s"',;&]+`),
	// Command-line flags: --rcon-password value, -password "value".
	regexp.MustCompile(`(?i)(-` + secretKey + `\s+")[^"]*`),
	regexp.MustCompile(`(?i)(-` + secretKey + `\s+)[^\s"']+`),
	// Authorization: Bearer <token>.
	regexp.MustCompile(`(?i)(\bBearer\s+)[A-Za-z0-9._~+/=-]+`),
	// User info of a URL: scheme://user:password@host.
	regexp.MustCompile(`(?i)(\b[a-z][a-z0-9+.-]*://)[^\s/@]+@`),
}

// Masker masks lines with the built-in list and the admin patterns.
type Masker struct{ admin []*regexp.Regexp }

// Compile checks admin patterns: at most MaxPatterns, each RE2 with at
// most MaxPatternRunes characters; the error names the first bad one.
func Compile(patterns []string) (*Masker, error) {
	if len(patterns) > MaxPatterns {
		return nil, fmt.Errorf("at most %d patterns", MaxPatterns)
	}
	m := &Masker{}
	for i, p := range patterns {
		if p == "" || utf8.RuneCountInString(p) > MaxPatternRunes {
			return nil, fmt.Errorf("pattern %d: 1 to %d characters", i+1, MaxPatternRunes)
		}
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("pattern %d: not a valid RE2 expression", i+1)
		}
		m.admin = append(m.admin, re)
	}
	return m, nil
}

// Line masks one line: built-in values keep their name, an admin pattern
// is replaced as a whole.
func (m *Masker) Line(s string) string {
	for _, re := range builtin {
		s = re.ReplaceAllString(s, "${1}"+Replacement)
	}
	for _, re := range m.admin {
		s = re.ReplaceAllLiteralString(s, Replacement)
	}
	return s
}

// Service masks lines after the current settings; it compiles the admin
// patterns again only when they change.
type Service struct {
	st *store.Store

	mu   sync.Mutex
	raw  string
	last *Masker
}

// New returns the masking of the panel.
func New(st *store.Store) *Service { return &Service{st: st, last: &Masker{}} }

// Lines masks the lines in place and returns them, unless the RCON admin
// switched the masking off. Without readable settings it masks with the
// built-in list: the switch fails closed.
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
	m := s.masker(ctx)
	for i, l := range lines {
		lines[i] = m.Line(l)
	}
	return lines
}

func (s *Service) masker(ctx context.Context) *Masker {
	var patterns []string
	raw := ""
	if err := s.st.GlobalSetting(ctx, SettingPatterns, &patterns); err == nil {
		b, _ := json.Marshal(patterns)
		raw = string(b)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if raw == s.raw {
		return s.last
	}
	m, err := Compile(patterns)
	if err != nil {
		// Stored patterns passed the same check; keep the built-in list.
		m = &Masker{}
	}
	s.raw, s.last = raw, m
	return m
}

// ErrPatterns marks an invalid list of admin patterns (400, UI-022).
var ErrPatterns = errors.New("invalid mask patterns")

// CheckPatterns is the settings rule of SettingPatterns.
func CheckPatterns(raw json.RawMessage) (any, error) {
	var patterns []string
	if err := json.Unmarshal(raw, &patterns); err != nil {
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
