// SPDX-License-Identifier: AGPL-3.0-or-later

// Package commands is the command pipeline (Design 3.3, Overview 4.2):
// clean the input, split it into tokens, check wrappers and the command
// list, apply the rate limit, mark, audit, send over the one connection
// of the server and return the answer.
package commands

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/NightDragon1/rconductor/internal/profiles"
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

// CatalogEntry returns the catalog command whose path starts the command;
// of several, the longest path wins.
func CatalogEntry(tokens []string, catalog []profiles.Command, caseSensitive bool) (profiles.Command, bool) {
	var best profiles.Command
	found := false
	for _, c := range catalog {
		path := Tokens(c.Path)
		if Allowed(tokens, path, caseSensitive) && (!found || len(path) > len(Tokens(best.Path))) {
			best, found = c, true
		}
	}
	return best, found
}

// CheckParams checks the arguments after a catalog path against its
// parameters for moderators and supporters (revision 2.4 candidate 19):
// a player is one token, a choice one of its values, a number a whole
// number in range, text the rest of the line; required parameters must be
// there. Further tokens pass, as after any list entry (CMD-004). It
// returns the first parameter that does not fit and why.
func CheckParams(args []string, params []profiles.Param, caseSensitive bool) (name, problem string, ok bool) {
	for i, p := range params {
		if i >= len(args) {
			if p.Optional {
				return "", "", true
			}
			return p.Name, "missing", false
		}
		a := args[i]
		switch p.Type {
		case profiles.ParamText:
			return "", "", true // the rest of the line
		case profiles.ParamPlayer:
			// Tokens never hold white space; a player is one token.
		case profiles.ParamChoice:
			if !slices.ContainsFunc(p.Values, func(v string) bool { return equal(a, v, caseSensitive) }) {
				return p.Name, "not one of " + strings.Join(p.Values, ", "), false
			}
		case profiles.ParamNumber:
			n, err := strconv.Atoi(a)
			if err != nil {
				return p.Name, "not a whole number", false
			}
			if (p.Min != nil && n < *p.Min) || (p.Max != nil && n > *p.Max) {
				return p.Name, "out of range", false
			}
		}
	}
	return "", "", true
}
