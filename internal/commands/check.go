// SPDX-License-Identifier: AGPL-3.0-or-later

// Package commands is the command pipeline (Design 3.3, Overview 4.2):
// clean the input, split it into tokens, check wrappers and the command
// list, apply the rate limit, mark, audit, send over the one connection
// of the server and return the answer.
package commands

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Reasons of a refused command (Design 3.5).
const (
	ReasonInvalid        = "invalid"
	ReasonNotInList      = "not_in_list"
	ReasonWrapperBlocked = "wrapper_blocked"
	ReasonRateLimited    = "rate_limited"
)

// Syntax are the command rules of the server's game profile (CAT-005).
type Syntax struct {
	CaseSensitive bool
	StripPrefix   string
	MaxLength     int
	Wrappers      []string
}

// ErrInvalid means the command is empty or too long after cleaning.
var ErrInvalid = errors.New("the command is empty or too long")

// Clean removes line breaks and every other control character, so that
// one input is exactly one command at the game server (CMD-006, T-005);
// tabs become spaces. It trims the input, removes the profile's prefix
// once and checks the length (CAT-005).
func Clean(input string, syn Syntax) (string, error) {
	var b strings.Builder
	for _, r := range input {
		switch {
		case r == '\t':
			b.WriteRune(' ')
		case unicode.IsControl(r), r == ' ', r == ' ', r == utf8.RuneError:
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	cmd := strings.TrimSpace(b.String())
	if syn.StripPrefix != "" {
		cmd = strings.TrimSpace(strings.TrimPrefix(cmd, syn.StripPrefix))
	}
	if cmd == "" || (syn.MaxLength > 0 && utf8.RuneCountInString(cmd) > syn.MaxLength) {
		return "", ErrInvalid
	}
	return cmd, nil
}

// Tokens splits a cleaned command at white space.
func Tokens(cmd string) []string { return strings.Fields(cmd) }

func equal(a, b string, caseSensitive bool) bool {
	if caseSensitive {
		return a == b
	}
	return strings.EqualFold(a, b)
}

// IsWrapper reports whether the command name is a wrapper of the profile,
// such as Minecraft's execute; wrappers are always refused for moderators
// and supporters, whatever their list says (CMD-005, D-026).
func IsWrapper(tokens []string, syn Syntax) bool {
	if len(tokens) == 0 {
		return false
	}
	for _, w := range syn.Wrappers {
		if equal(tokens[0], w, syn.CaseSensitive) {
			return true
		}
	}
	return false
}

// Allowed reports whether a list entry permits the command: the entry is
// a path of whole tokens at the start of the command, and everything
// after it passes unchanged (CMD-004). "op" does not permit "opfoo".
func Allowed(tokens, path []string, caseSensitive bool) bool {
	if len(path) == 0 || len(path) > len(tokens) {
		return false
	}
	for i, p := range path {
		if !equal(tokens[i], p, caseSensitive) {
			return false
		}
	}
	return true
}

// AllowedByList reports whether any entry of the list permits the command.
func AllowedByList(tokens []string, list []string, caseSensitive bool) bool {
	for _, entry := range list {
		if Allowed(tokens, Tokens(entry), caseSensitive) {
			return true
		}
	}
	return false
}
