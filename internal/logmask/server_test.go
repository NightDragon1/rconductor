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
// still masks: without group 1 the whole match, with a group 2 inside
// group 1 up to the end of the match.
func TestPatternGroupsFailClosed(t *testing.T) {
	for pattern, want := range map[string]string{
		`x=\S+`:            "a •••",
		`(k=)?x=\S+`:       "a •••",
		`(x(=))\S+`:        "a x=•••",
		`(x=)\S+(?:(@)z)?`: "a x=•••",
	} {
		m := (*Masker)(nil).WithPatterns([]*regexp.Regexp{regexp.MustCompile(pattern)})
		if got := m.Line("a x=v@y"); got != want {
			t.Errorf("%s: got %q, want %q", pattern, got, want)
		}
	}
}

// TestLinesBudget: once the budget of a batch is spent, every line left
// is withheld, never passed unmasked; the clock is injected.
func TestLinesBudget(t *testing.T) {
	var mu sync.Mutex
	now := time.Unix(0, 0)
	clock := func() time.Time { // every call is 10 ms later
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(10 * time.Millisecond)
		return now
	}
	lines := []string{"token=a", "token=b", "token=c", "token=d", "token=e"}
	got, withheld := (*Masker)(nil).Lines(lines, Budget{Max: 25 * time.Millisecond, Now: clock})
	want := []string{"token=•••", "token=•••", Withheld, Withheld, Withheld}
	if withheld != 3 || strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, %d withheld", got, withheld)
	}
	if got, withheld := (*Masker)(nil).Lines(lines, Budget{Max: time.Hour, Now: clock}); withheld != 0 || got[4] != "token=•••" {
		t.Errorf("within the budget: %q, %d", got, withheld)
	}
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
	if withheld == 0 {
		t.Errorf("no line withheld after %s", took)
	}
	for i, l := range got {
		if strings.Contains(l, "=a") {
			t.Fatalf("line %d left unmasked", i)
		}
	}
}
