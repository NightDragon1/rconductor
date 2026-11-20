// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package diag

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/agenthub"
	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/logmask"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
)

// hubs answers logs.tail per agent.
type hubs map[string]*hub

func (h hubs) RequestData(ctx context.Context, id, typ string, payload any, collect func(agentproto.Envelope)) (agentproto.Result, error) {
	return h[id].RequestData(ctx, id, typ, payload, collect)
}

const (
	agentOn  = "00000000-0000-4000-8000-0000000000a1"
	agentOff = "00000000-0000-4000-8000-0000000000a2"
)

// exportFixture is a store with an RCON admin and the built-in masking.
func exportFixture(t *testing.T, h Agents) (*store.Store, *Service, users.Request) {
	t.Helper()
	st := storetest.New(t)
	m, err := logmask.New(nil, logmask.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(st, nil)
	s.Use(h, builtin{m})
	admin := users.Request{Actor: perm.Actor{ID: storetest.CreateUser(t, st, storetest.User{Pseudonym: "a", Role: string(perm.RCONAdmin)}), Role: perm.RCONAdmin}}
	return st, s, admin
}

func exec(t *testing.T, st *store.Store, q string, args ...any) {
	t.Helper()
	if _, err := st.DB.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// TestExportLogsBlackedOut covers NFR-018, D-090 and D-091: the system log
// of the period and the log of every active agent are in the package, each
// file headed by the notice; an agent offline is noted; a known secret, a
// hostname and an IP in quoted values are in no file, neither in JSON nor
// in the text format.
func TestExportLogsBlackedOut(t *testing.T) {
	h := hubs{
		agentOn: {code: agentproto.CodeOK, lines: []string{
			`{"level":"error","msg":"dial \"play.example.com\" at \"192.168.1.7\" password: \"agentpw1\""}`,
			`time=2026-11-20T11:00:00.000Z level=warn msg="dial \"play.example.com\" at \"192.168.1.7\" password: \"agentpw2\""`,
		}},
		agentOff: {err: agenthub.ErrNotConnected},
	}
	st, s, admin := exportFixture(t, h)
	exec(t, st, `INSERT INTO agents (id, name, public_key, fingerprint, status, remote, created_at) VALUES (?, 'on', x'00', 'fp1', 'active', '', 't'), (?, 'off', x'01', 'fp2', 'active', '', 't')`, agentOn, agentOff)
	exec(t, st, `INSERT INTO servers (id, name, host, port, protocol, created_at, updated_at) VALUES ('s1', 'lobby', 'mc.example.lan', 25575, 'source-rcon', 't', 't')`)
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	ts := now.Add(-30 * time.Minute).Format(store.TimeLayout)
	for _, line := range []string{
		`{"level":"error","msg":"rcon \"mc.example.lan\" at \"10.20.30.40\" password: \"hunter2\"","answer":"{\"token\":\"tok-in-json\"}"}`,
		`time=2026-11-20T11:30:00.000Z level=error msg="rcon \"mc.example.lan\" at \"10.20.30.40\" password: \"hunter3\""`,
		`{"level":"info","msg":"kept line"}`,
	} {
		exec(t, st, `INSERT INTO system_log (ts, level, code, text, size) VALUES (?, 'error', 'x.y', ?, 1)`, ts, line)
	}
	exec(t, st, `INSERT INTO system_log (ts, level, code, text, size) VALUES (?, 'info', 'x.y', 'outside the period', 1)`, now.Add(-2*time.Hour).Format(store.TimeLayout))
	b, err := s.Export(context.Background(), admin, now.Add(-time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, b)
	sys, on, off := files["system-log.txt"], files["agents/"+agentOn+".log"], files["agents/"+agentOff+".log"]
	for name, text := range map[string]string{"system-log.txt": sys, "on": on, "off": off} {
		if !strings.HasPrefix(text, Notice+"\n") {
			t.Errorf("%s does not start with the notice: %q", name, text)
		}
	}
	if !strings.Contains(sys, "kept line") || strings.Contains(sys, "outside the period") || strings.Count(sys, "rcon ") != 2 {
		t.Errorf("system log:\n%s", sys)
	}
	if strings.Count(on, "dial ") != 2 || !strings.Contains(off, "offline") {
		t.Errorf("agent logs:\n%s\n%s", on, off)
	}
	var all strings.Builder
	for _, text := range files {
		all.WriteString(text)
	}
	got := all.String()
	for _, secret := range []string{"hunter2", "hunter3", "tok-in-json", "agentpw1", "agentpw2",
		"mc.example.lan", "play.example.com", "10.20.30.40", "192.168.1.7"} {
		if strings.Contains(got, secret) {
			t.Errorf("%q in the package:\n%s", secret, got)
		}
	}
	if !strings.Contains(sys, "host-") || !strings.Contains(sys, "ip-") || !strings.Contains(on, "host-") || !strings.Contains(on, "ip-") {
		t.Errorf("pseudonyms:\n%s\n%s", sys, on)
	}
}

// TestExportShortened covers D-090: above diagnostics.max_bytes the oldest
// lines of the system log stay out, the newest stay, and the package and
// the audit say so.
func TestExportShortened(t *testing.T) {
	st, s, admin := exportFixture(t, nil)
	exec(t, st, `INSERT INTO settings (id, key, value, updated_at) VALUES ('d1', ?, ?, 't')`, users.SettingDiagnosticsMaxBytes, fmt.Sprint(users.MinDiagnosticsMaxBytes))
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	tx, err := st.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	const rows = 1100 // a little more than 1 MiB
	for i := range rows {
		text := fmt.Sprintf(`{"level":"info","msg":"row %d %s"}`, i, strings.Repeat("x", 1000))
		ts := now.Add(-time.Hour + time.Duration(i)*time.Second).Format(store.TimeLayout)
		if _, err := tx.Exec(`INSERT INTO system_log (ts, level, code, text, size) VALUES (?, 'info', 'x.y', ?, ?)`, ts, text, len(text)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	b, err := s.Export(context.Background(), admin, now.Add(-time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	size := 0
	for name, text := range unzip(t, b) {
		size += len(name) + len(text)
	}
	sys := unzip(t, b)["system-log.txt"]
	if !strings.Contains(sys, "Shortened by diagnostics.max_bytes") || strings.Contains(sys, `"row 0 `) ||
		!strings.Contains(sys, fmt.Sprintf(`"row %d `, rows-1)) || size > users.MinDiagnosticsMaxBytes+512 {
		t.Errorf("shortened: %d bytes, head %q", size, sys[:min(len(sys), 300)])
	}
	var payload string
	if err := st.DB.QueryRow(`SELECT payload FROM audit_log WHERE action = 'diagnostics.exported' AND result = 'ok'`).Scan(&payload); err != nil ||
		!strings.Contains(payload, `"shortened":true`) || strings.Contains(payload, "row") {
		t.Errorf("audit = %s, %v", payload, err)
	}
}

// TestExportParallel covers D-090: at most diagnostics.parallel packages
// at the same time, more are ErrBusy; role and period come first; under
// parallel requests every answer is a package or ErrBusy and every slot
// is given back.
func TestExportParallel(t *testing.T) {
	st, s, admin := exportFixture(t, nil)
	exec(t, st, `INSERT INTO settings (id, key, value, updated_at) VALUES ('d2', ?, '1', 't')`, users.SettingDiagnosticsParallel)
	ctx := context.Background()
	now := time.Now().UTC()
	release, ok := s.acquire(ctx)
	if !ok {
		t.Fatal("no slot")
	}
	mod := users.Request{Actor: perm.Actor{ID: admin.Actor.ID, Role: perm.Moderator}}
	if _, err := s.Export(ctx, admin, now.Add(-time.Hour), now); !errors.Is(err, ErrBusy) {
		t.Errorf("second export = %v, want ErrBusy", err)
	}
	if _, err := s.Export(ctx, mod, now.Add(-time.Hour), now); !errors.Is(err, users.ErrForbidden) {
		t.Errorf("moderator while busy = %v", err)
	}
	inv := (*users.InvalidError)(nil)
	if _, err := s.Export(ctx, admin, now.Add(-8*24*time.Hour), now); !errors.As(err, &inv) {
		t.Errorf("8 days while busy = %v", err)
	}
	release()
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Go(func() { _, errs[i] = s.Export(ctx, admin, now.Add(-time.Hour), now) })
	}
	wg.Wait()
	done := 0
	for _, err := range errs {
		switch {
		case err == nil:
			done++
		case !errors.Is(err, ErrBusy):
			t.Errorf("parallel export = %v", err)
		}
	}
	if done == 0 || s.running != 0 {
		t.Errorf("packages %d, slots in use %d", done, s.running)
	}
}
