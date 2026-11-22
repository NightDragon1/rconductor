// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package logmask

import (
	"strings"
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
// quotes are masked up to the closing quote, also with an escaped quote inside, in
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

// TestC147N3EscapedDoubleQuotes: a value in double quotes ends at the
// first quote that is not escaped.
func TestC147N3EscapedDoubleQuotes(t *testing.T) {
	for in, want := range map[string]string{
		`{"password":"ab\"cd","port":1}`: `{"password":"•••","port":1}`,
		`-password "a\"b c" -nogui`:      `-password "•••" -nogui`,
	} {
		if got := (*Masker)(nil).Line(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

// TestC147N4ListCost: a line at the protocol bound full of short list
// matches is masked in one pass and does not grow past the bound.
func TestC147N4ListCost(t *testing.T) {
	line := strings.Repeat("token=x -token y u://b@ ", agentproto.MaxLogLine/24)
	start := time.Now()
	got := (*Masker)(nil).Line(line)
	took := time.Since(start)
	if strings.Contains(got, "=x") || len(got) > agentproto.MaxLogLine {
		t.Errorf("got %d bytes, starts %q", len(got), got[:40])
	}
	t.Logf("one line of %d bytes with %d list matches: %s", len(line), 3*(agentproto.MaxLogLine/24), took)
	if took > lineBound {
		t.Errorf("one line took %s", took)
	}
}

// TestC238ANSIBetweenNameAndSeparator: escape sequences of a colored log
// around a name or a separator keep the value masked (UI-022).
func TestC238ANSIBetweenNameAndSeparator(t *testing.T) {
	for in, want := range map[string]string{
		"\x1b[1mpassword\x1b[0m=hunter2 next":            "\x1b[1mpassword\x1b[0m=••• next",
		"RCON_PASSWORD\x1b[0m: \x1b[33mhunter2\x1b[0m x": "RCON_PASSWORD\x1b[0m: \x1b[33m••• x",
		"token\x1b[2m=\x1b[0m\"two words\" x":            "token\x1b[2m=\x1b[0m\"•••\" x",
		"password=\x1b(B\x1b[mhunter2 x":                 "password=\x1b(B\x1b[m••• x",
		"password=\x1b[38:2:1:2:3mhunter2 x":             "password=\x1b[38:2:1:2:3m••• x",
		"password\x1b(B: hunter2 x":                      "password\x1b(B: ••• x",
		"'password\x1b[0m=a b' x":                        "'password\x1b[0m=•••' x",
	} {
		if got := (*Masker)(nil).Line(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

// TestC239MoreForms: a Python dict, escaped JSON, :=, the DSN of a Go SQL
// driver and an @ in the user info of a URL keep the value masked (UI-022).
func TestC239MoreForms(t *testing.T) {
	for in, want := range map[string]string{
		`{'password': 'hun ter2', 'user': 'steve'}`:     `{'password': '•••', 'user': 'steve'}`,
		`msg="{\"password\":\"hun ter2\",\"port\":1}"`:  `msg="{\"password\":\"•••\",\"port\":1}"`,
		`password := hunter2 next`:                      `password := ••• next`,
		`RCON_PASSWORD:="two words" next`:               `RCON_PASSWORD:="•••" next`,
		`dsn root:hun@ter2@tcp(db:3306)/game`:           `dsn root:•••@tcp(db:3306)/game`,
		`GET https://steve:p@ss@example.com/x?mail=a@b`: `GET https://•••@example.com/x?mail=a@b`,
		`password=a\b next`:                             `password=••• next`,
		`mail steve@example.com at 12:30 for help`:      `mail steve@example.com at 12:30 for help`,
		`password==hunter2 next`:                        `password==••• next`,
		`password: =abc next`:                           `password: ••• next`,
		`password: ==abc next`:                          `password: ••• next`,
		`password: =\x next`:                            `password: ••• next`,
		`password====x next`:                            `password==••• next`,
		`password=abc\`:                                 `password=•••`,
		`msg="{\"password\":\"a\\\"b\"}"`:               `msg="{\"password\":\"•••\"}"`,
		`dsn root:hun/ter2@/game`:                       `dsn root:•••@/game`,
		`postgres://app:pa#ss@db:5432/x`:                `postgres://•••@db:5432/x`,
		`postgres://app:pa?ss@db:5432/x`:                `postgres://•••@db:5432/x`,
	} {
		if got := (*Masker)(nil).Line(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

// TestC424ColorResetKept (UI-022): an escape sequence right after the
// separator stays as it is, only the value is masked, so the colors of the
// line do not run on.
func TestC424ColorResetKept(t *testing.T) {
	m, err := New(nil, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{
		"RCON_PASSWORD=\x1b[0m\"hunter2\"":     "RCON_PASSWORD=\x1b[0m\"•••\"",
		"RCON_PASSWORD=\x1b[0mhunter2":         "RCON_PASSWORD=\x1b[0m•••",
		"password: \x1b[32m\"abc\"\x1b[0m end": "password: \x1b[32m\"•••\"\x1b[0m end",
	} {
		if got := m.Line(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
