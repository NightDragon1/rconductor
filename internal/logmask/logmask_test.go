// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package logmask

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/agentproto"
)

// TestBuiltin covers UI-022 and D-088 a): the built-in list masks the
// values of secret keys and flags, Bearer tokens and the user info of URLs,
// keeps names and separators, and leaves other lines as they are.
func TestBuiltin(t *testing.T) {
	var m *Masker
	for in, want := range map[string]string{
		"exec java --rcon-password hunter2 -jar server.jar": "exec java --rcon-password ••• -jar server.jar",
		"--rcon-password=hunter2":                           "--rcon-password=•••",
		"password=hunter2 user=steve":                       "password=••• user=steve",
		"RCON_PASSWORD=hunter2":                             "RCON_PASSWORD=•••",
		`{"db_password": "hunter 2", "port": 25575}`:        `{"db_password": "•••", "port": 25575}`,
		`-password "two words" -nogui`:                      `-password "•••" -nogui`,
		"API_KEY='k-1' TOKEN=t.2":                           "API_KEY='•••' TOKEN=•••",
		"passwd=x&user=y":                                   "passwd=•••&user=y",
		"Authorization: Bearer eyJhbGciOi.x-y_z":            "Authorization: Bearer •••",
		"GET https://steve:hunter2@example.com/x":           "GET https://•••@example.com/x",
		"password=\"open quote to the end":                  "password=\"•••",
		"[Server] Done (5.2s)! For help, type \"help\"":     "[Server] Done (5.2s)! For help, type \"help\"",
		"wrong password for steve":                          "wrong password for steve",
	} {
		if got := m.Line(in); got != want {
			t.Errorf("%q\n got %q\nwant %q", in, got, want)
		}
	}
}

// TestC139B1ShellTraceQuotes: values of shell traces (set -x) in single
// quotes are masked up to the closing quote, also with '\” inside, in
// every quoted form of the list (C139 R2-3, R3-3).
func TestC139B1ShellTraceQuotes(t *testing.T) {
	var m *Masker
	for in, want := range map[string]string{
		"+ java --rcon-password 'pa$$ word' -nogui": "+ java --rcon-password '•••' -nogui",
		"+ export 'RCON_PASSWORD=a b c'":            "+ export 'RCON_PASSWORD=•••'",
		"+ run -token 'x;y&z'":                      "+ run -token '•••'",
		`+ export 'RCON_PASSWORD=it'\''s' next`:     `+ export 'RCON_PASSWORD=•••' next`,
		`+ run -token 'it'\''s' next`:               `+ run -token '•••' next`,
		`+ env RCON_PASSWORD='it'\''s' next`:        `+ env RCON_PASSWORD='•••' next`,
	} {
		if got := m.Line(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

// TestCutAtLineBound: a line the panel cuts at MaxLogLine still has its
// secret masked, also when the cut falls inside the value.
func TestCutAtLineBound(t *testing.T) {
	head := strings.Repeat("x", agentproto.MaxLogLine-20) + " password="
	lines, _ := agentproto.LimitLines([]string{head + "hunter2hunter2hunter2"})
	got := (*Masker)(nil).Line(lines[0])
	if strings.Contains(got, "hunter") || !strings.HasSuffix(got, "password=•••") {
		t.Errorf("cut line ends %q", got[len(got)-40:])
	}
}

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

// lineBound bounds one worst-case line, also under the race detector.
const lineBound = time.Second

// TestLineCost covers D-088 (cost per line bounded and measured): a line
// at the protocol bound against the most and longest texts, all folded
// and overlapping everywhere, takes time linear in the line per text. The
// measured time is in the PR.
func TestLineCost(t *testing.T) {
	texts := make([]Text, MaxTexts)
	for i := range texts {
		texts[i] = Text{Value: strings.Repeat("a", MaxTextRunes-i), Fold: i%2 == 0}
	}
	m, err := New(texts, Limits{Texts: MaxTexts, TextRunes: MaxTextRunes})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("a", agentproto.MaxLogLine-12) + " password=x"
	start := time.Now()
	got := m.Line(line)
	took := time.Since(start)
	if got != Replacement+" password="+Replacement {
		t.Errorf("got %q", got)
	}
	t.Logf("one line of %d bytes, %d texts of up to %d characters: %s", len(line), len(texts), MaxTextRunes, took)
	if took > lineBound {
		t.Errorf("one line took %s, bound %s", took, lineBound)
	}
}

// TestParallel: one masker serves parallel lines.
func TestParallel(t *testing.T) {
	m, err := New([]Text{{Value: "hunter2", Fold: true}}, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			for range 100 {
				if got := m.Line("x HUNTER2 password=y"); got != "x ••• password=•••" {
					t.Errorf("got %q", got)
					return
				}
			}
		})
	}
	wg.Wait()
}
