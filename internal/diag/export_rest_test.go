// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package diag

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/users"
)

// TestC333DomainsAndMailHost: a domain value is pseudonymized whatever its
// top-level domain, in JSON and in the text format, and the SMTP host
// is a known host (D-090).
func TestC333DomainsAndMailHost(t *testing.T) {
	p := newPseudonyms([]string{"smtp.mailer.fr"})
	got := p.apply([]string{"msg", "send via smtp.mailer.fr", `level=info domain=other.gg msg=x`})
	for _, leak := range []string{"smtp.mailer.fr", "other.gg"} {
		if strings.Contains(strings.Join(got, " "), leak) {
			t.Errorf("%s left in %q", leak, got)
		}
	}
	if !strings.Contains(got[2], "domain=host-") {
		t.Errorf("apply = %q", got)
	}

	st, s, admin := exportFixture(t, nil)
	exec(t, st, `UPDATE smtp_settings SET host = 'relay.mailer.nl' WHERE id = 1`)
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	exec(t, st, `INSERT INTO system_log (ts, level, code, text, size) VALUES (?, 'warn', 'x.y', ?, 1)`,
		now.Add(-time.Minute).Format(store.TimeLayout), `{"level":"warn","msg":"dial relay.mailer.nl:587 failed"}`)
	b, err := s.Export(context.Background(), admin, now.Add(-time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range unzip(t, b) {
		if strings.Contains(text, "relay.mailer.nl") {
			t.Errorf("SMTP host in %s", name)
		}
	}
}

// blocking answers once all agents asked at the same time; asked one
// after another, the first waits for its deadline.
type blocking struct {
	n, want int32
	all     chan struct{}
}

func (b *blocking) RequestData(ctx context.Context, id, typ string, payload any, collect func(agentproto.Envelope)) (agentproto.Result, error) {
	if atomic.AddInt32(&b.n, 1) == b.want {
		close(b.all)
	}
	select {
	case <-b.all:
	case <-ctx.Done():
		return agentproto.Result{}, ctx.Err()
	}
	e, _ := agentproto.New(agentproto.TypeLogLines, "", agentproto.LogLines{Container: agentproto.SelfContainer, Lines: []string{"line of " + id}})
	collect(e)
	return agentproto.Result{Code: agentproto.CodeOK}, nil
}

// TestC336AgentsAtOnce: the logs of all agents are asked for at the same
// time under one deadline (D-091).
func TestC336AgentsAtOnce(t *testing.T) {
	b := &blocking{want: 2, all: make(chan struct{})}
	st, s, admin := exportFixture(t, b)
	exec(t, st, `INSERT INTO agents (id, name, public_key, fingerprint, status, remote, created_at) VALUES (?, 'on', x'00', 'fp1', 'active', '', 't'), (?, 'off', x'01', 'fp2', 'active', '', 't')`, agentOn, agentOff)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := time.Now().UTC()
	out, err := s.Export(ctx, admin, now.Add(-time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, out)
	for _, id := range []string{agentOn, agentOff} {
		if !strings.Contains(files["agents/"+id+".log"], "line of "+id) {
			t.Errorf("log of %s = %q", id, files["agents/"+id+".log"])
		}
	}
}

// TestC334BudgetCoversAgentLogs: the agent logs count toward and are cut
// by diagnostics.max_bytes, newest lines kept; with no room left the
// system log is not read and its head says so; the package has the fixed
// file list (D-091).
func TestC334BudgetCoversAgentLogs(t *testing.T) {
	var lines []string
	for i := range agentproto.MaxLogBytes / 1024 {
		lines = append(lines, fmt.Sprintf("agent line %d %s", i, strings.Repeat("y", 1000)))
	}
	// three agents of about 512 KiB each, over the minimum of 1 MiB
	a3 := "00000000-0000-4000-8000-0000000000a3"
	h := hubs{agentOn: {code: agentproto.CodeOK, lines: lines}, agentOff: {code: agentproto.CodeOK, lines: lines}, a3: {code: agentproto.CodeOK, lines: lines}}
	st, s, admin := exportFixture(t, h)
	exec(t, st, `INSERT INTO agents (id, name, public_key, fingerprint, status, remote, created_at) VALUES (?, 'a1', x'00', 'fp1', 'active', '', 't'),
		(?, 'a2', x'01', 'fp2', 'active', '', 't'), (?, 'a3', x'02', 'fp3', 'active', '', 't')`, agentOn, agentOff, a3)
	exec(t, st, `INSERT INTO settings (id, key, value, updated_at) VALUES ('d1', ?, ?, 't')`, users.SettingDiagnosticsMaxBytes, fmt.Sprint(users.MinDiagnosticsMaxBytes))
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	exec(t, st, `INSERT INTO system_log (ts, level, code, text, size) VALUES (?, 'info', 'x.y', 'a stored line', 1)`, now.Add(-time.Minute).Format(store.TimeLayout))
	b, err := s.Export(context.Background(), admin, now.Add(-time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, b)
	names := make([]string, 0, len(files))
	size := 0
	for name, text := range files {
		names = append(names, name)
		size += len(name) + len(text)
	}
	slices.Sort(names)
	want := []string{"agents.json", "agents/" + agentOn + ".log", "agents/" + agentOff + ".log", "agents/" + a3 + ".log", "profiles.json", "servers.json", "settings.json", "system-log.txt", "versions.json"}
	if !slices.Equal(names, want) {
		t.Errorf("files = %v, want %v", names, want)
	}
	agent := files["agents/"+a3+".log"]
	if !strings.Contains(agent, "Shortened by diagnostics.max_bytes") || !strings.Contains(agent, fmt.Sprintf("agent line %d ", len(lines)-1)) ||
		strings.Contains(agent, "agent line 0 ") {
		t.Errorf("agent log not cut from the old end: %q", agent[:min(len(agent), 200)])
	}
	sys := files["system-log.txt"]
	if !strings.HasPrefix(sys, Notice+"\n") || !strings.Contains(sys, "Shortened by diagnostics.max_bytes") || strings.Contains(sys, "a stored line") {
		t.Errorf("system log = %q", sys)
	}
	if size > users.MinDiagnosticsMaxBytes+1024 {
		t.Errorf("package = %d bytes, over the budget %d", size, users.MinDiagnosticsMaxBytes)
	}
}

// TestC382DomainByKey (D-090): in JSON every text under the key domain,
// in arrays too, becomes its host pseudonym; a value "domain" leaves the
// next key as it is.
func TestC382DomainByKey(t *testing.T) {
	p := newPseudonyms(nil)
	got := p.domains(`{"a":{"domain":["one.xyz",["two.xyz"],""]},"domain":"three.xyz","msg":"domain","next":"plain-word"}`)
	for _, leak := range []string{"one.xyz", "two.xyz", "three.xyz"} {
		if strings.Contains(got, leak) {
			t.Errorf("%s left in %s", leak, got)
		}
	}
	if !strings.Contains(got, `"next":"plain-word"`) || !strings.Contains(got, `"msg":"domain"`) || !strings.Contains(got, `["host-1",["host-2"],""]`) {
		t.Errorf("domains = %s", got)
	}
	nested := p.domains(`{"domain":{"Domain":"four.xyz","name":["five.xyz"]},"DOMAIN":"six.xyz"}`)
	for _, leak := range []string{"four.xyz", "five.xyz", "six.xyz"} {
		if strings.Contains(nested, leak) {
			t.Errorf("%s left in %s", leak, nested)
		}
	}
	if line := "domain one.xyz in text"; p.domains(line) != line {
		t.Errorf("text line changed: %s", p.domains(line))
	}
}

// TestC382DomainThroughExport (D-090): a JSON key domain with an unknown
// top-level domain leaves neither the system log nor an agent log of the
// package unpseudonymized.
func TestC382DomainThroughExport(t *testing.T) {
	h := &hub{code: agentproto.CodeOK, lines: []string{`{"level":"info","domain":"agent-side.xyz"}`}}
	st, s, admin := exportFixture(t, h)
	exec(t, st, `INSERT INTO agents (id, name, public_key, fingerprint, status, remote, created_at) VALUES ('00000000-0000-4000-8000-0000000000b1', 'a', x'00', 'fp', 'active', '', 't')`)
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	exec(t, st, `INSERT INTO system_log (ts, level, code, text, size) VALUES (?, 'warn', 'x.y', ?, 1)`,
		now.Add(-time.Minute).Format(store.TimeLayout), `{"level":"warn","domain":["panel-side.xyz"]}`)
	b, err := s.Export(context.Background(), admin, now.Add(-time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for name, text := range unzip(t, b) {
		for _, leak := range []string{"agent-side.xyz", "panel-side.xyz"} {
			if strings.Contains(text, leak) {
				t.Errorf("%s in %s", leak, name)
			}
		}
		seen += strings.Count(text, `"domain":"host-`) + strings.Count(text, `"domain":["host-`)
	}
	if seen != 2 {
		t.Errorf("%d domain pseudonyms, want 2", seen)
	}
}
