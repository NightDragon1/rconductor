// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package logmask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// TestBuiltin covers UI-022 and D4: the built-in list masks the values of
// secret keys and flags, Bearer tokens and the user info of URLs, keeps
// names and separators, and leaves other lines as they are.
func TestBuiltin(t *testing.T) {
	m := &Masker{}
	for in, want := range map[string]string{
		"exec java --rcon-password hunter2 -jar server.jar": "exec java --rcon-password ••• -jar server.jar",
		"--rcon-password=hunter2":                           "--rcon-password=•••",
		"password=hunter2 user=steve":                       "password=••• user=steve",
		"RCON_PASSWORD=hunter2":                             "RCON_PASSWORD=•••",
		`{"db_password": "hunter 2", "port": 25575}`:        `{"db_password": "•••", "port": 25575}`,
		`-password "two words" -nogui`:                      `-password "•••" -nogui`,
		"secret: abc; next":                                 "secret: •••; next",
		"API_KEY='k-1' TOKEN=t.2":                           "API_KEY='•••' TOKEN=•••",
		"passwd=x&user=y":                                   "passwd=•••&user=y",
		"Authorization: Bearer eyJhbGciOi.x-y_z":            "Authorization: Bearer •••",
		"GET https://steve:hunter2@example.com/x":           "GET https://•••@example.com/x",
		// Shell traces (set -x) quote values with space or $ in '...'.
		"+ exec java --rcon-password 'pa$$ word' -nogui": "+ exec java --rcon-password '•••' -nogui",
		"+ export 'RCON_PASSWORD=a b'":                   "+ export 'RCON_PASSWORD=•••'",
		"password=\"open quote to the end":               "password=\"•••",
		"[Server] Done (5.2s)! For help, type \"help\"":  "[Server] Done (5.2s)! For help, type \"help\"",
		"wrong password for steve":                       "wrong password for steve",
		"":                                               "",
	} {
		if got := m.Line(in); got != want {
			t.Errorf("%q\n got %q\nwant %q", in, got, want)
		}
	}
}

// TestCutAtLineBound: a line the panel cuts at MaxLogLine still has its
// secret masked, also when the cut falls inside the value.
func TestCutAtLineBound(t *testing.T) {
	head := strings.Repeat("x", agentproto.MaxLogLine-20) + " password="
	lines, _ := agentproto.LimitLines([]string{head + "hunter2hunter2hunter2"})
	got := (&Masker{}).Line(lines[0])
	if strings.Contains(got, "hunter") || !strings.HasSuffix(got, "password=•••") {
		t.Errorf("cut line ends %q", got[len(got)-40:])
	}
}

// TestAdminPatterns covers D4: at most 20 RE2 patterns of at most 256
// characters; a match is replaced as a whole.
func TestAdminPatterns(t *testing.T) {
	m, err := Compile([]string{`steam_[0-9]{4}`, `(?i)license-key \S+`})
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Line("id steam_1234 and License-Key AB-CD"); got != "id ••• and •••" {
		t.Errorf("got %q", got)
	}
	for name, bad := range map[string][]string{
		"too many": make([]string, MaxPatterns+1),
		"empty":    {""},
		"too long": {strings.Repeat("a", MaxPatternRunes+1)},
		"not RE2":  {`(?<=x)y`},
		"broken":   {`(`},
		"backrefs": {`(a)\1`},
	} {
		if name == "too many" {
			for i := range bad {
				bad[i] = "a"
			}
		}
		if _, err := Compile(bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
		raw, _ := json.Marshal(bad)
		if _, err := CheckPatterns(raw); !errors.Is(err, ErrPatterns) {
			t.Errorf("%s: rule error %v, want ErrPatterns", name, err)
		}
	}
	if _, err := Compile([]string{strings.Repeat("ä", MaxPatternRunes)}); err != nil {
		t.Errorf("256 characters (512 bytes): %v", err)
	}
	if _, err := CheckPatterns(json.RawMessage(`"x"`)); !errors.Is(err, ErrPatterns) {
		t.Errorf("not a list: %v", err)
	}
}

// TestLongLine: a line of 1 MiB with many candidates is masked in linear
// time (RE2), well within a request; under the race detector a shorter
// line keeps the run short.
func TestLongLine(t *testing.T) {
	m, err := Compile([]string{`(a|aa)+b`, `x*x*x*y`})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("password=a token=b ", longLineBytes/19)
	start := time.Now()
	got := m.Line(line + strings.Repeat("a", 1<<16) + strings.Repeat("x", 1<<16))
	if d := time.Since(start); d > longLineBound {
		t.Errorf("1 MiB line took %s", d)
	}
	if strings.Contains(got, "password=a ") {
		t.Error("a value stayed")
	}
}

// TestC139B2AdminPatternGrowth: patterns of the RCON admin cannot blow a
// line up: they run in one pass over the input, never over earlier
// replacements, and the result keeps the line bound.
func TestC139B2AdminPatternGrowth(t *testing.T) {
	dots := make([]string, MaxPatterns)
	for i := range dots {
		dots[i] = "."
	}
	m, err := Compile(dots)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("a", agentproto.MaxLogLine)
	if got := m.Line(line); len(got) > agentproto.MaxLogLine || !utf8.ValidString(got) {
		t.Errorf("line grew to %d bytes (valid %v)", len(got), utf8.ValidString(got))
	}
	if got := m.Line("ab"); got != Replacement+Replacement {
		t.Errorf("one pass: %q", got)
	}
}

// TestService covers UI-022: the masking is on without a setting, off when
// the RCON admin switches it off, and follows changed patterns.
func TestService(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	s := New(st)
	set := func(key, value string) {
		t.Helper()
		if _, err := st.DB.ExecContext(ctx, `INSERT INTO settings (id, key, value, scope_type, scope_id, updated_at) VALUES (?, ?, ?, 'global', '', 't')
			ON CONFLICT (key, scope_type, scope_id) DO UPDATE SET value = excluded.value`, key, key, value); err != nil {
			t.Fatal(err)
		}
	}
	line := func() string { return s.Lines(ctx, []string{"password=hunter2 steam_1234"})[0] }
	if got := line(); got != "password=••• steam_1234" {
		t.Errorf("default: %q", got)
	}
	set(SettingPatterns, `["steam_[0-9]+"]`)
	if got := line(); got != "password=••• •••" {
		t.Errorf("with pattern: %q", got)
	}
	set(SettingPatterns, `[]`)
	if got := line(); got != "password=••• steam_1234" {
		t.Errorf("pattern removed: %q", got)
	}
	set(SettingEnabled, `false`)
	if got := line(); got != "password=hunter2 steam_1234" {
		t.Errorf("switched off: %q", got)
	}
	// A context that ended: every line is withheld, none goes out as it
	// is (fail closed).
	set(SettingEnabled, `true`)
	done, cancel := context.WithCancel(ctx)
	cancel()
	if got := s.Lines(done, []string{"password=hunter2", "plain"}); got[0] != Replacement || got[1] != Replacement {
		t.Errorf("ended context: %q", got)
	}
	// Patterns that cannot be read keep the last ones.
	set(SettingPatterns, `["steam_[0-9]+"]`)
	_ = line()
	set(SettingPatterns, `not json`)
	if got := line(); got != "password=••• •••" {
		t.Errorf("unreadable patterns: %q", got)
	}
	var nilService *Service
	if got := nilService.Lines(ctx, []string{"password=x"}); got[0] != "password=x" {
		t.Errorf("nil service: %q", got)
	}
}

// TestServiceParallel: lines are masked while the patterns change, without
// a race and never with the plain value.
func TestServiceParallel(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	s := New(st)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for range 50 {
				if got := s.Lines(ctx, []string{"password=hunter2"})[0]; strings.Contains(got, "hunter2") {
					t.Errorf("plain value: %q", got)
					return
				}
			}
		})
		wg.Go(func() {
			v := fmt.Sprintf(`["p%d_[0-9]+"]`, i)
			if _, err := st.DB.ExecContext(ctx, `INSERT INTO settings (id, key, value, scope_type, scope_id, updated_at) VALUES ('p', ?, ?, 'global', '', 't')
				ON CONFLICT (key, scope_type, scope_id) DO UPDATE SET value = excluded.value`, SettingPatterns, v); err != nil {
				t.Errorf("set patterns: %v", err)
			}
		})
	}
	wg.Wait()
}

// TestC139B1ShellTraceQuotes: values a shell trace (set -x) puts in single
// quotes are masked up to the closing quote.
func TestC139B1ShellTraceQuotes(t *testing.T) {
	m := &Masker{}
	for in, want := range map[string]string{
		"+ java --rcon-password 'pa$$ word' -nogui": "+ java --rcon-password '•••' -nogui",
		"+ export 'RCON_PASSWORD=a b c'":            "+ export 'RCON_PASSWORD=•••'",
		"+ run -token 'x;y&z'":                      "+ run -token '•••'",
	} {
		if got := m.Line(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

// TestC139B3Bounds: an admin pattern with a large program is refused, and
// lines left when the time of a call ends are withheld, not sent as they
// are.
func TestC139B3Bounds(t *testing.T) {
	if _, err := Compile([]string{`.{1000}`}); err == nil {
		t.Error("a pattern of more than MaxPatternInsts instructions was taken")
	}
	if _, err := Compile([]string{`[a-z]{1,40}@[a-z]{1,40}`}); err != nil {
		t.Errorf("a pattern of normal size: %v", err)
	}
	s := New(storetest.New(t))
	done, cancel := context.WithCancel(context.Background())
	cancel()
	for _, got := range s.Lines(done, []string{"password=hunter2", "plain"}) {
		if got != Replacement {
			t.Errorf("after the end: %q", got)
		}
	}
}

// TestC139R2LineCost: a pattern that searches to the line end for every
// match cannot make one line expensive: past MaxAdminMatches matches the
// line is withheld as a whole.
func TestC139R2LineCost(t *testing.T) {
	m, err := Compile([]string{`x(?:.*y)?`})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	got := m.Line(strings.Repeat("x", agentproto.MaxLogLine))
	if d := time.Since(start); d > DefaultTimeout {
		t.Errorf("one line took %s", d)
	}
	if got != Replacement {
		t.Errorf("line with many matches = %.40q", got)
	}
	if got := m.Line("x1 x2"); got != "•••1 •••2" {
		t.Errorf("few matches: %q", got)
	}
}

// TestC139R2PatternForms: patterns are checked in the form they run in, so
// \Q without \E is refused without a panic and without the pattern text in
// the error; patterns with empty matches inside a line are refused.
func TestC139R2PatternForms(t *testing.T) {
	for _, list := range [][]string{{`\Qhunter2`}, {`\Qhunter2`, `b`}, {`b`, `\Qhunter2`}} {
		_, err := Compile(list)
		if err == nil || strings.Contains(err.Error(), "hunter2") {
			t.Errorf("%q: %v", list, err)
		}
	}
	for _, p := range []string{`\b`, `\bfoo|\b`, `(?m)$`} {
		if _, err := Compile([]string{p}); err == nil {
			t.Errorf("%q: empty matches taken", p)
		}
	}
	if got := (&Masker{}).Line("+ export 'RCON_PASSWORD=it'\\''s'"); got != "+ export 'RCON_PASSWORD=•••'" {
		t.Errorf("quote inside the value: %q", got)
	}
}

// TestC139R2Deadline: lines left after the time limit are withheld; a
// limit below MinTimeout counts as the default.
func TestC139R2Deadline(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	s := New(st)
	clock := time.Unix(0, 0)
	s.now = func() time.Time { clock = clock.Add(300 * time.Millisecond); return clock }
	if _, err := st.DB.ExecContext(ctx, `INSERT INTO settings (id, key, value, scope_type, scope_id, updated_at) VALUES ('t', ?, '"50ms"', 'global', '', 't')`, SettingTimeout); err != nil {
		t.Fatal(err)
	}
	got := s.Lines(ctx, []string{"a", "b", "c", "d", "e"})
	// Deadline 1s after the first reading; lines at 0.3 s steps after it.
	want := []string{"a", "b", "c", Replacement, Replacement}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("lines = %q, want %q", got, want)
	}
}
