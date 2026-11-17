// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package logmask

import (
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/agentproto"
)

// TestTexts covers D-088 c): fixed texts, no expressions, exact or without
// case of A to Z; every place is masked, overlapping places as their union.
func TestTexts(t *testing.T) {
	m, err := New([]Text{{Value: "s3cr3t.v4lue"}, {Value: "hunter2", Fold: true}, {Value: "abab"}, {Value: "(?i).*"}}, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{
		"join s3cr3t.v4lue now":         "join ••• now",
		"join S3CR3T.V4LUE now":         "join S3CR3T.V4LUE now",
		"HUNTER2 and Hunter2 hunter2":   "••• and ••• •••",
		"x ababab y":                    "x ••• y",
		"no regex: anything goes":       "no regex: anything goes",
		"literal (?i).* here":           "literal ••• here",
		"hunter2s3cr3t.v4lue":           "•••",
		"Ünïcödé hunter2 stays":         "Ünïcödé ••• stays",
		"password=hunter2 then hunter2": "password=••• then •••",
	} {
		if got := m.Line(in); got != want {
			t.Errorf("%q\n got %q\nwant %q", in, got, want)
		}
	}
}

// TestTextBounds: a text has MinTextRunes to Limits.TextRunes characters
// on one line; at most Limits.Texts texts; the error never holds a text.
func TestTextBounds(t *testing.T) {
	lim := Limits{Texts: 3, TextRunes: 8}
	for name, texts := range map[string][]Text{
		"too short":     {{Value: "abc"}},
		"too long":      {{Value: "abcdefghi"}},
		"line break":    {{Value: "ab\ncd"}},
		"invalid UTF-8": {{Value: "ab\xffcd"}},
		"too many":      {{Value: "aaaa"}, {Value: "bbbb"}, {Value: "cccc"}, {Value: "dddd"}},
	} {
		_, err := New(texts, lim)
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		for _, x := range texts {
			if strings.Contains(err.Error(), x.Value) {
				t.Errorf("%s: error holds the text: %v", name, err)
			}
		}
	}
	if _, err := New([]Text{{Value: "äöüß"}, {Value: "abcdefgh"}, {Value: "1234"}}, lim); err != nil {
		t.Errorf("at the bounds: %v", err)
	}
	many := make([]Text, MaxTexts+1)
	for i := range many {
		many[i] = Text{Value: strings.Repeat("x", MinTextRunes) + string(rune('a'+i%26)) + string(rune('a'+i/26))}
	}
	if _, err := New(many, Limits{Texts: 10000, TextRunes: 10000}); err == nil {
		t.Error("more than MaxTexts accepted")
	}
}

// lineBound bounds one worst-case line: 1 s, and 5 s under the race
// detector, which slowed it past 1 s on a loaded CI runner.
var lineBound = func() time.Duration {
	if bi, ok := debug.ReadBuildInfo(); ok && slices.Contains(bi.Settings, debug.BuildSetting{Key: "-race", Value: "true"}) {
		return 5 * time.Second
	}
	return time.Second
}()

// TestLineCost covers D-088 (cost per line bounded and measured) and
// C147 N4: a line at the protocol bound full of short list matches and of
// a run that every text covers, against the most and longest texts, half
// of them without case. The line does not grow before the texts search it.
func TestLineCost(t *testing.T) {
	texts := make([]Text, MaxTexts)
	for i := range texts {
		texts[i] = Text{Value: strings.Repeat("a", MaxTextRunes-i), Fold: i%2 == 0}
	}
	m, err := New(texts, Limits{Texts: MaxTexts, TextRunes: MaxTextRunes})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("token=x -token y u://b@ ", 170) + strings.Repeat("a", agentproto.MaxLogLine-170*24-1)
	start := time.Now()
	got := m.Line(line)
	took := time.Since(start)
	if strings.Contains(got, "=x") || strings.Contains(got, "aaaa") || len(got) > agentproto.MaxLogLine {
		t.Errorf("got %d bytes: %q", len(got), got[len(got)-30:])
	}
	t.Logf("one line of %d bytes, 510 list matches, %d texts of up to %d characters: %s", len(line), len(texts), MaxTextRunes, took)
	if took > lineBound {
		t.Errorf("one line took %s, bound %s", took, lineBound)
	}
}

// TestParallel: one masker serves parallel lines.
func TestParallel(t *testing.T) {
	m, _ := New([]Text{{Value: "hunter2", Fold: true}}, DefaultLimits)
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if got := m.Line("x HUNTER2 password=y"); got != "x ••• password=•••" {
				t.Errorf("got %q", got)
			}
		})
	}
	wg.Wait()
}

// TestC147N1TextsSeeTheLineAsItCame: a text that reaches past a value of
// the built-in list is masked as a whole; every source searches the
// original line.
func TestC147N1TextsSeeTheLineAsItCame(t *testing.T) {
	m, err := New([]Text{{Value: "two words"}, {Value: "hunter,2"}, {Value: "pw;x;y"}}, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{
		"--rcon-password two words -nogui": "--rcon-password ••• -nogui",
		"password=hunter,2 next":           "password=••• next",
		"token=pw;x;y end":                 "token=••• end",
	} {
		if got := m.Line(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

// TestC147N2TextAtTheLineBound: the start of a text at the end of a line
// cut at the bound is masked.
func TestC147N2TextAtTheLineBound(t *testing.T) {
	m, err := New([]Text{{Value: "s3cr3t-value"}}, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	lines, _ := agentproto.LimitLines([]string{strings.Repeat("x", agentproto.MaxLogLine-6) + " s3cr3t-value"})
	// The mark itself may be cut at the bound; nothing of the text is left.
	if got := m.Line(lines[0]); strings.Contains(got, "s3") || !strings.HasSuffix(got, " •") {
		t.Errorf("line ends %q", got[len(got)-20:])
	}
}

// TestC147N5MessageCost measures one log.lines message of the agent at its
// byte bound (MaxLogBytes in lines of MaxLogLine bytes), worst-case lines,
// with the default and with the highest limits for the texts. The panel
// masks a message once before the cache; the time per message bounds the
// throughput of one agent connection.
func TestC147N5MessageCost(t *testing.T) {
	n := agentproto.MaxLogBytes / agentproto.MaxLogLine
	line := strings.Repeat("token=x -token y u://b@ ", 170) + strings.Repeat("a", agentproto.MaxLogLine-170*24-1)
	lines := make([]string, n)
	for i := range lines {
		lines[i] = line
	}
	for _, lim := range []Limits{DefaultLimits, {Texts: MaxTexts, TextRunes: MaxTextRunes}} {
		texts := make([]Text, lim.Texts)
		for i := range texts {
			texts[i] = Text{Value: strings.Repeat("a", lim.TextRunes-i%(lim.TextRunes-MinTextRunes)), Fold: i%2 == 0}
		}
		m, err := New(texts, lim)
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		for _, l := range lines {
			if got := m.Line(l); strings.Contains(got, "aaaa") {
				t.Fatalf("texts %d: line not masked", lim.Texts)
			}
		}
		took := time.Since(start)
		t.Logf("one message of %d lines of %d bytes, %d texts of up to %d characters: %s", n, len(line), lim.Texts, lim.TextRunes, took)
		if took > time.Duration(n)*lineBound {
			t.Errorf("one message took %s, bound %s", took, time.Duration(n)*lineBound)
		}
	}
}
