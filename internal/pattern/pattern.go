// SPDX-License-Identifier: AGPL-3.0-or-later

// Package pattern runs regular expressions from game profiles on foreign
// data (SEC-003): RE2 semantics with linear run time, a limit on the
// expression, on the lines and on the time spent.
package pattern

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Limits of patterns and inputs.
const (
	MaxExprLen     = 512
	MaxLines       = 1000
	MaxLineLen     = 4096
	DefaultTimeout = 200 * time.Millisecond
)

// ErrTimeout means the search took longer than allowed; what was found
// until then is not used.
var ErrTimeout = errors.New("pattern search timed out")

// Pattern is a compiled, limited expression.
type Pattern struct {
	re      *regexp.Regexp
	timeout time.Duration
}

// Compile accepts an RE2 expression of at most MaxExprLen characters with
// at least groups capture groups.
func Compile(expr string, groups int) (*Pattern, error) {
	if expr == "" || len(expr) > MaxExprLen {
		return nil, fmt.Errorf("a pattern has 1 to %d characters", MaxExprLen)
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}
	if re.NumSubexp() < groups {
		return nil, fmt.Errorf("the pattern needs %d capture group(s)", groups)
	}
	return &Pattern{re: re, timeout: DefaultTimeout}, nil
}

// WithTimeout returns a copy with another time limit.
func (p *Pattern) WithTimeout(d time.Duration) *Pattern {
	c := *p
	c.timeout = d
	return &c
}

// String returns the expression.
func (p *Pattern) String() string { return p.re.String() }

// FirstMatch searches the lines of text in order and returns the capture
// groups of the first matching line. Only the first MaxLines lines, each
// cut to MaxLineLen bytes, are searched; the search stops at the time
// limit or when ctx ends.
func (p *Pattern) FirstMatch(ctx context.Context, text string) ([]string, bool, error) {
	deadline := time.Now().Add(p.timeout)
	lines := 0
	for line := range strings.Lines(text) {
		if lines == MaxLines {
			break
		}
		lines++
		if time.Now().After(deadline) {
			return nil, false, ErrTimeout
		}
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		line = strings.TrimRight(line, "\r\n")
		if len(line) > MaxLineLen {
			line = line[:MaxLineLen]
		}
		if m := p.re.FindStringSubmatch(line); m != nil {
			return m[1:], true, nil
		}
	}
	return nil, false, nil
}
