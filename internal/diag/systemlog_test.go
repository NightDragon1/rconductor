// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package diag

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/agenthub"
	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/logmask"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
)

// hub answers logs.tail with lines, a result code or an error, and keeps
// the order it got.
type hub struct {
	lines []string
	code  string
	err   error
	got   agentproto.LogsTail
}

func (h *hub) RequestData(_ context.Context, _, typ string, payload any, collect func(agentproto.Envelope)) (agentproto.Result, error) {
	if h.err != nil {
		return agentproto.Result{}, h.err
	}
	h.got = payload.(agentproto.LogsTail)
	e, _ := agentproto.New(agentproto.TypeLogLines, "", agentproto.LogLines{Container: h.got.Container, Lines: h.lines})
	collect(e)
	return agentproto.Result{Code: h.code}, nil
}

// builtin masks with the built-in list of D-088.
type builtin struct{ m *logmask.Masker }

func (b builtin) Lines(lines []string, _ [][]string) ([]string, int) {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = b.m.Line(l)
	}
	return out, 0
}

// TestAgentLog covers D-091 and UI-025: the log of an agent is the own
// container (@self) over logs.tail, masked as container logs, from the
// level up; a read is audited without content; an agent without the
// capability or outside a container is unsupported, an agent that is not
// connected unavailable.
func TestAgentLog(t *testing.T) {
	st := storetest.New(t)
	m, err := logmask.New(nil, logmask.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	h := &hub{code: agentproto.CodeOK, lines: []string{
		`{"level":"info","msg":"paired"}`, `{"level":"error","msg":"docker gone","detail":"token=abc12345"}`, "plain line",
	}}
	s := NewService(st, nil)
	s.Use(h, builtin{m})
	ctx := context.Background()
	admin := users.Request{Actor: perm.Actor{ID: storetest.CreateUser(t, st, storetest.User{Pseudonym: "a", Role: string(perm.RCONAdmin)}), Role: perm.RCONAdmin}}
	const agent = "00000000-0000-4000-8000-000000000001"
	p, err := s.SystemLog(ctx, admin, LogQuery{Source: agent, Level: "warn", Limit: 50})
	var texts []string
	for _, e := range p.Items {
		texts = append(texts, e.Text)
	}
	got := strings.Join(texts, "|")
	if err != nil || h.got.Container != agentproto.SelfContainer || h.got.Lines != 50 ||
		!strings.Contains(got, "docker gone") || strings.Contains(got, "paired") || !strings.Contains(got, "plain line") || strings.Contains(got, "abc12345") {
		t.Errorf("agent log = %q, %v, order %+v", got, err, h.got)
	}
	var audited int
	_ = st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE action = 'system_log.viewed' AND result = 'ok' AND coalesce(payload, '') NOT LIKE '%docker%'`).Scan(&audited)
	if audited != 1 {
		t.Errorf("audited reads = %d, want 1", audited)
	}
	for _, c := range []struct {
		code string
		err  error
		want error
	}{
		{agentproto.CodeForbidden, nil, ErrAgentUnsupported},
		{"", agenthub.ErrNotCapable, ErrAgentUnsupported},
		{"", agenthub.ErrNotConnected, ErrAgentUnavailable},
		{agentproto.CodeError, nil, ErrAgentFailed},
	} {
		h.code, h.err = c.code, c.err
		if _, err := s.SystemLog(ctx, admin, LogQuery{Source: agent, Level: "info", Limit: 5, After: 1}); !errors.Is(err, c.want) {
			t.Errorf("%s %v = %v, want %v", c.code, c.err, err, c.want)
		}
	}
	if _, err := NewService(st, nil).SystemLog(ctx, admin, LogQuery{Source: agent, Level: "info", Limit: 5, After: 1}); !errors.Is(err, ErrAgentUnavailable) {
		t.Errorf("without hub = %v", err)
	}
}

// TestC175AgentJSONLinesMasked: the agent logs JSON; a value in quotes and
// a JSON text inside a value are masked as they were logged (D-088, D-091).
func TestC175AgentJSONLinesMasked(t *testing.T) {
	st := storetest.New(t)
	m, err := logmask.New(nil, logmask.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	h := &hub{code: agentproto.CodeOK, lines: []string{
		`{"level":"error","msg":"login password: \"abc12345\""}`,
		`{"level":"warn","detail":"{\"token\":\"tok98765\"}"}`,
	}}
	s := NewService(st, nil)
	s.Use(h, builtin{m})
	admin := users.Request{Actor: perm.Actor{ID: storetest.CreateUser(t, st, storetest.User{Pseudonym: "a", Role: string(perm.RCONAdmin)}), Role: perm.RCONAdmin}}
	p, err := s.SystemLog(context.Background(), admin, LogQuery{Source: "00000000-0000-4000-8000-000000000001", Level: "info", Limit: 50})
	var texts []string
	for _, e := range p.Items {
		texts = append(texts, e.Text)
	}
	got := strings.Join(texts, "\n")
	if err != nil || len(texts) != 2 || strings.Contains(got, "abc12345") || strings.Contains(got, "tok98765") || !strings.Contains(got, "login password") {
		t.Errorf("agent log = %q, %v", got, err)
	}
}
