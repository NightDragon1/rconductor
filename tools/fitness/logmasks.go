// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Budgets of the masking patterns of the shipped profiles (D-088 b), per
// line of lineLen bytes: one pattern, and the stack of maxStack profiles
// that cost most on the line. A server has at most 16 profiles.
const (
	lineLen      = 8 << 10
	maxStack     = 16
	patternSpend = 10 * time.Millisecond
	stackSpend   = 50 * time.Millisecond
)

type budget struct{ pattern, stack time.Duration }

// checkLogMasks runs every masking pattern of the profiles in dir/profiles
// on lines that are hard for it and for the others, each pattern alone and
// the costliest stack together, against the budgets.
func checkLogMasks(dir string, b budget) ([]finding, error) {
	files, err := filepath.Glob(filepath.Join(dir, "profiles", "*.yaml"))
	if err != nil {
		return nil, err
	}
	type pat struct {
		profile, src string
		re           *regexp.Regexp
	}
	var found []finding
	var pats []pat
	for _, f := range files {
		data, err := os.ReadFile(filepath.Clean(f))
		if err != nil {
			return nil, err
		}
		var p struct {
			ID       string   `yaml:"id"`
			LogMasks []string `yaml:"log_masks"`
		}
		if err := yaml.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		for _, m := range p.LogMasks {
			re, err := regexp.Compile(m)
			if err != nil {
				found = append(found, finding{"logmask", p.ID + " " + m, "does not compile"})
				continue
			}
			pats = append(pats, pat{p.ID, m, re})
		}
	}
	lines := hardLines()
	for _, p := range pats {
		lines = append(lines, literalLine(p.src))
	}
	for _, line := range lines {
		perProfile := map[string]time.Duration{}
		for _, p := range pats {
			d := spend(p.re, line)
			perProfile[p.profile] += d
			if d > b.pattern {
				found = append(found, finding{"logmask", p.profile + " " + p.src, fmt.Sprintf("%s per line, budget %s", d, b.pattern)})
			}
		}
		costs := slices.Collect(func(yield func(time.Duration) bool) {
			for _, d := range perProfile {
				if !yield(d) {
					return
				}
			}
		})
		slices.Sort(costs)
		slices.Reverse(costs)
		var sum time.Duration
		for _, d := range costs[:min(len(costs), maxStack)] {
			sum += d
		}
		if sum > b.stack {
			found = append(found, finding{"logmask", "stack", fmt.Sprintf("%s per line for the costliest %d profiles, budget %s", sum, maxStack, b.stack)})
		}
	}
	return found, nil
}

// spend is the least of three runs of the search the masker makes.
func spend(re *regexp.Regexp, line string) time.Duration {
	least := time.Duration(1<<63 - 1)
	for range 3 {
		start := time.Now()
		re.FindAllStringSubmatchIndex(line, -1)
		least = min(least, time.Since(start))
	}
	return least
}

// hardLines are lines that are hard for the patterns of secrets: many
// short matches, one long run, many names without values.
func hardLines() []string {
	return []string{
		fill("token=x -token y u://b@ "),
		fill("a"),
		fill("pass="),
		fill("aB3_=: "),
		fill(`"key": "`),
	}
}

// literalLine repeats the literal parts of a pattern, so that it matches
// or nearly matches all along the line.
func literalLine(pattern string) string {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return ""
	}
	var parts []string
	var walk func(*syntax.Regexp)
	walk = func(r *syntax.Regexp) {
		switch {
		case r.Op == syntax.OpLiteral && r.Flags&syntax.FoldCase != 0:
			parts = append(parts, strings.ToLower(string(r.Rune)))
		case r.Op == syntax.OpLiteral:
			parts = append(parts, string(r.Rune))
		}
		for _, s := range r.Sub {
			walk(s)
		}
	}
	walk(re)
	return fill(strings.Join(parts, "") + "x ")
}

// fill repeats s to lineLen bytes.
func fill(s string) string {
	if s == "" {
		s = " "
	}
	return strings.Repeat(s, lineLen/len(s)+1)[:lineLen]
}
