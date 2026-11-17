// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package logmask

import (
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/profiles"
)

// TestProfilePatterns covers D-088 b): the patterns of the profiles mask
// after group 1, in the same pass as the list and the texts; the masker
// they are added to stays as it was.
func TestProfilePatterns(t *testing.T) {
	base, err := New([]Text{{Value: "t0p-s3cr3t"}}, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	m := base.WithPatterns([]*regexp.Regexp{regexp.MustCompile(`(?i)(adminpass\s*=\s*)\S+`), regexp.MustCompile(`(k=)\S+(@)`)})
	for in, want := range map[string]string{
		"AdminPass=hunter2 token=abc t0p-s3cr3t": "AdminPass=••• token=••• •••",
		"k=user@host":                            "k=•••@host",
		"adminpass=":                             "adminpass=",
	} {
		if got := m.Line(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
	if got := base.Line("AdminPass=hunter2"); got != "AdminPass=hunter2" {
		t.Errorf("base masker changed: %q", got)
	}
	if got := (*Masker)(nil).WithPatterns([]*regexp.Regexp{regexp.MustCompile(`(k=)\S+`)}).Line("k=v token=x"); got != "k=••• token=•••" {
		t.Errorf("nil base: %q", got)
	}
}

// TestPatternGroupsFailClosed: a pattern the profile check would refuse
// still masks: the whole match without group 1, with text before it or
// with nothing between group 1 and group 2 or the end; a group 2 inside
// group 1 counts as missing.
func TestPatternGroupsFailClosed(t *testing.T) {
	for pattern, want := range map[string]string{
		`x=\S+`:            "a •••",
		`(k=)?x=\S+`:       "a •••",
		`(x(=))\S+`:        "a x=•••",
		`(x=)\S+(?:(@)z)?`: "a x=•••",
		`x=\S+(y)`:         "a •••",
		`(x=)(v)\S+`:       "a •••",
		`(x=\S+)`:          "a •••",
	} {
		m := (*Masker)(nil).WithPatterns([]*regexp.Regexp{regexp.MustCompile(pattern)})
		if got := m.Line("a x=v@y"); got != want {
			t.Errorf("%s: got %q, want %q", pattern, got, want)
		}
	}
}

// stepClock is an injected clock: every call is one step later.
func stepClock(step time.Duration) func() time.Time {
	var mu sync.Mutex
	now := time.Unix(0, 0)
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(step)
		return now
	}
}

// TestLinesBudget: once the budget of a batch is spent, every line left
// is withheld, never passed unmasked. A line asks the clock once and once
// per built-in pattern.
func TestLinesBudget(t *testing.T) {
	perLine := time.Duration(1+len(builtin)) * time.Millisecond
	lines := []string{"token=a", "token=b", "token=c", "token=d", "token=e"}
	got, withheld := (*Masker)(nil).Lines(lines, Budget{Max: 2*perLine + 5*time.Millisecond, Now: stepClock(time.Millisecond)})
	want := []string{"token=•••", "token=•••", Withheld, Withheld, Withheld}
	if withheld != 3 || strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, %d withheld", got, withheld)
	}
	for name, b := range map[string]Budget{
		"within":       {Max: time.Hour, Now: stepClock(time.Millisecond)},
		"no budget":    {Max: 0, Now: stepClock(time.Millisecond)},
		"clock behind": {Max: time.Millisecond, Now: stepClock(-time.Second)},
	} {
		if got, withheld := (*Masker)(nil).Lines(lines, b); withheld != 0 || got[4] != "token=•••" {
			t.Errorf("%s: %q, %d", name, got, withheld)
		}
	}
	if got, withheld := (*Masker)(nil).Lines(nil, Budget{}); len(got) != 0 || withheld != 0 {
		t.Errorf("empty batch: %q, %d", got, withheld)
	}
}

// TestLinesBudgetInsideLine: the budget is asked between the patterns of
// one line, so a line with many patterns stops in the middle and is
// withheld as a whole.
func TestLinesBudgetInsideLine(t *testing.T) {
	res := make([]*regexp.Regexp, 50)
	for i := range res {
		res[i] = regexp.MustCompile(`(k=)\S+`)
	}
	m := (*Masker)(nil).WithPatterns(res)
	got, withheld := m.Lines([]string{"k=v1", "k=v2"}, Budget{Max: 20 * time.Millisecond, Now: stepClock(time.Millisecond)})
	if withheld != 2 || got[0] != Withheld || got[1] != Withheld {
		t.Errorf("got %q, %d withheld", got, withheld)
	}
}

// TestWithPatternsShared: two maskers made from the same one with
// patterns stay apart, also used in parallel.
func TestWithPatternsShared(t *testing.T) {
	b1 := (*Masker)(nil).WithPatterns([]*regexp.Regexp{regexp.MustCompile(`(a=)\S+`)})
	b2 := b1.WithPatterns([]*regexp.Regexp{regexp.MustCompile(`(b=)\S+`)})
	b3 := b1.WithPatterns([]*regexp.Regexp{regexp.MustCompile(`(c=)\S+`)})
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if got := b2.Line("a=1 b=2 c=3"); got != "a=••• b=••• c=3" {
				t.Errorf("b2: %q", got)
			}
			if got, _ := b3.Lines([]string{"a=1 b=2 c=3"}, Budget{Max: time.Hour}); got[0] != "a=••• b=2 c=•••" {
				t.Errorf("b3: %q", got[0])
			}
		})
	}
	wg.Wait()
}

// TestLinesFlood measures a batch at the byte bound of the agent (64 lines
// of 8 KiB) with a pattern whose short matches each search to the end of
// the line: the budget ends it, and no line leaves unmasked. This bounds
// the cost of any stack of profiles per batch.
func TestLinesFlood(t *testing.T) {
	m := (*Masker)(nil).WithPatterns([]*regexp.Regexp{regexp.MustCompile(`(pw=)\S+(?: .*END)?`)})
	line := strings.Repeat("pw=a ", agentproto.MaxLogLine/5)
	lines := make([]string, agentproto.MaxLogBytes/agentproto.MaxLogLine)
	for i := range lines {
		lines[i] = line
	}
	const budget = 100 * time.Millisecond
	start := time.Now()
	got, withheld := m.Lines(lines, Budget{Max: budget})
	took := time.Since(start)
	t.Logf("%d lines of %d bytes: %s, %d withheld", len(lines), len(line), took, withheld)
	if withheld < len(lines)-2 {
		t.Errorf("%d withheld after %s, want at least %d", withheld, took, len(lines)-2)
	}
	for i, l := range got {
		if strings.Contains(l, "=a") {
			t.Fatalf("line %d left unmasked", i)
		}
	}
}

// TestBuiltinPassesProfileCheck: every expression of the built-in list
// would pass the check of profile patterns, so list and profiles follow one
// rule of groups.
func TestBuiltinPassesProfileCheck(t *testing.T) {
	for _, re := range builtin {
		if _, bad := profiles.CompileLogMasks([]string{re.String()}); len(bad) > 0 {
			t.Errorf("%s fails the profile check", re)
		}
	}
}
