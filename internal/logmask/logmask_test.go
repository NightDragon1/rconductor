// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package logmask

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

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
		"GET https://steve:hunter2@example.com/x":           "GET https://•••example.com/x",
		"password=\"open quote to the end":                  "password=\"•••",
		"[Server] Done (5.2s)! For help, type \"help\"":     "[Server] Done (5.2s)! For help, type \"help\"",
		"wrong password for steve":                          "wrong password for steve",
		"":                                                  "",
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
	var nilService *Service
	if got := nilService.Lines(ctx, []string{"password=x"}); got[0] != "password=x" {
		t.Errorf("nil service: %q", got)
	}
}
