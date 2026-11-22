// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package diag

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/logmask"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
)

// TestC325AgentLevelKnownOnly: the level of an agent line is foreign data;
// only info, warn and error are kept, in any case, and anything else is
// none; a rate-limited agent is ErrAgentBusy (D-091, SEC-006).
func TestC325AgentLevelKnownOnly(t *testing.T) {
	st := storetest.New(t)
	m, err := logmask.New(nil, logmask.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	h := &hub{code: agentproto.CodeOK, lines: []string{
		`{"level":"WARN","msg":"upper"}`, `{"level":"<script>","msg":"odd"}`, `{"level":"INFO","msg":"low"}`,
	}}
	s := NewService(st, nil)
	s.Use(h, builtin{m})
	ctx := context.Background()
	admin := users.Request{Actor: perm.Actor{ID: storetest.CreateUser(t, st, storetest.User{Pseudonym: "a", Role: string(perm.RCONAdmin)}), Role: perm.RCONAdmin}}
	const agent = "00000000-0000-4000-8000-000000000001"
	p, err := s.SystemLog(ctx, admin, LogQuery{Source: agent, Level: "warn", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	var levels []string
	for _, e := range p.Items {
		levels = append(levels, e.Level)
	}
	if strings.Join(levels, ",") != "warn," {
		t.Errorf("levels = %q, want warn and none (INFO below warn left out)", levels)
	}
	h.code = agentproto.CodeRateLimited
	if _, err := s.SystemLog(ctx, admin, LogQuery{Source: agent, Level: "info", Limit: 5, After: 1}); !errors.Is(err, ErrAgentBusy) {
		t.Errorf("rate limited = %v, want ErrAgentBusy", err)
	}
}

// TestC382AuditShortenedByAgentLog: the audit of an export says shortened
// also when only an agent log was cut (D-090).
func TestC382AuditShortenedByAgentLog(t *testing.T) {
	var lines []string
	// full logs: lines of the longest size up to the byte bound per agent
	for i := range agentproto.MaxLogBytes / agentproto.MaxLogLine {
		l := fmt.Sprintf("agent line %d ", i)
		lines = append(lines, l+strings.Repeat("y", agentproto.MaxLogLine-len(l)))
	}
	// two agents: the second is cut, the system log keeps a little room
	ids := []string{agentOn, agentOff}
	h := hubs{}
	for _, id := range ids {
		h[id] = &hub{code: agentproto.CodeOK, lines: lines}
	}
	st, s, admin := exportFixture(t, h)
	for i, id := range ids {
		exec(t, st, `INSERT INTO agents (id, name, public_key, fingerprint, status, remote, created_at) VALUES (?, ?, ?, ?, 'active', '', 't')`,
			id, fmt.Sprint("a", i), []byte{byte(i)}, fmt.Sprint("fp", i))
	}
	exec(t, st, `INSERT INTO settings (id, key, value, updated_at) VALUES ('d1', ?, ?, 't')`, users.SettingDiagnosticsMaxBytes, fmt.Sprint(users.MinDiagnosticsMaxBytes))
	now := time.Now().UTC()
	b, err := s.Export(context.Background(), admin, now.Add(-time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, b)
	if strings.Contains(files["system-log.txt"], "Shortened") || !strings.Contains(files["agents/"+agentOff+".log"], "Shortened") {
		t.Fatalf("want only the agent log cut: system log %q", files["system-log.txt"])
	}
	var payload string
	if err := st.DB.QueryRow(`SELECT payload FROM audit_log WHERE action = 'diagnostics.exported' AND result = 'ok'`).Scan(&payload); err != nil ||
		!strings.Contains(payload, `"shortened":true`) {
		t.Errorf("audit = %s, %v", payload, err)
	}
}
