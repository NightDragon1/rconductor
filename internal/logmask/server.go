// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package logmask

import (
	"regexp"
	"slices"
	"time"
)

// Withheld stands for a line that was not masked in time; it never
// leaves the panel unmasked (fail closed).
const Withheld = "••• (line withheld: masking took too long)"

// DefaultBudget is the time a batch of lines may take by default; a
// setting with a lower bound follows (UI-022, D-088).
const DefaultBudget = 250 * time.Millisecond

// WithPatterns returns a masker that also masks the patterns of the
// profiles of a server (D-088 b), as profiles.CompileLogMasks gives them;
// m stays as it is. A pattern of group 1 alone, which that check refuses,
// would mask nothing.
func (m *Masker) WithPatterns(res []*regexp.Regexp) *Masker {
	out := &Masker{}
	if m != nil {
		*out = *m
	}
	out.patterns = append(slices.Clip(out.patterns), res...)
	return out
}

// Budget bounds the time of a batch of lines; now is the clock, so that
// tests need no real time.
type Budget struct {
	Max time.Duration
	Now func() time.Time
}

// Lines masks a batch of lines, as the agent sends them, in order. Once
// the budget is spent, the line in work and every line left are replaced
// by Withheld; the number of these comes back for the log. The budget is
// asked between lines and between the patterns of a line, so one search
// of one pattern on one line is the most it can run over.
func (m *Masker) Lines(lines []string, b Budget) ([]string, int) {
	if b.Now == nil {
		b.Now = time.Now
	}
	if b.Max <= 0 {
		b.Max = DefaultBudget
	}
	out := make([]string, len(lines))
	start := b.Now()
	over := func() bool { return b.Now().Sub(start) > b.Max }
	for i, l := range lines {
		masked, stopped := "", over()
		if !stopped {
			masked, stopped = m.line(l, over)
		}
		if stopped {
			for j := i; j < len(lines); j++ {
				out[j] = Withheld
			}
			return out, len(lines) - i
		}
		out[i] = masked
	}
	return out, 0
}
