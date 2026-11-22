// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package diag

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

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

// TestC175AgentJSONLinesMasked: the agent logs JSON or text; a value in
// quotes and a JSON text inside a value are masked as they were logged, in
// both formats (D-088, D-091).
func TestC175AgentJSONLinesMasked(t *testing.T) {
	st := storetest.New(t)
	m, err := logmask.New(nil, logmask.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	h := &hub{code: agentproto.CodeOK, lines: []string{
		`{"level":"error","msg":"login password: \"abc12345\""}`,
		`{"level":"warn","detail":"{\"token\":\"tok98765\"}"}`,
		`time=2026-11-20T12:00:00.000Z level=error msg="login password: \"txt45678\""`,
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
	if err != nil || len(texts) != 3 || strings.Contains(got, "abc12345") || strings.Contains(got, "tok98765") ||
		strings.Contains(got, "txt45678") || !strings.Contains(got, "login password") {
		t.Errorf("agent log = %q, %v", got, err)
	}
}

// TestC179FirstReadAudited: the first read of a source in a session is
// audited even when it already carries After; polling after it is not,
// another source or session is audited again (D-091).
func TestC179FirstReadAudited(t *testing.T) {
	st := storetest.New(t)
	m, err := logmask.New(nil, logmask.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(st, nil)
	s.Use(&hub{code: agentproto.CodeOK, lines: []string{"line"}}, builtin{m})
	ctx := context.Background()
	id := storetest.CreateUser(t, st, storetest.User{Pseudonym: "a", Role: string(perm.RCONAdmin)})
	in := func(session string) users.Request {
		return users.Request{Actor: perm.Actor{ID: id, Role: perm.RCONAdmin}, SessionID: session}
	}
	const agent = "00000000-0000-4000-8000-000000000001"
	now := time.Now().UTC()
	panel := LogQuery{Source: "panel", From: now.Add(-time.Hour), To: now, Level: "info", Limit: 5, After: 1}
	viewed := func() (n int) {
		_ = st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE action = 'system_log.viewed' AND result = 'ok'`).Scan(&n)
		return n
	}
	for i, c := range []struct {
		req  users.Request
		q    LogQuery
		want int
	}{
		{in("s1"), panel, 1}, // first call with After
		{in("s1"), panel, 1}, // poll
		{in("s1"), LogQuery{Source: agent, Level: "info", Limit: 5, After: 1}, 2},                  // other source
		{in("s1"), LogQuery{Source: agent, Level: "info", Limit: 5, After: 7}, 2},                  // poll
		{in("s1"), LogQuery{Source: strings.ToUpper(agent), Level: "info", Limit: 5, After: 8}, 2}, // same agent, other spelling
		{in("s2"), panel, 3}, // other session
		{in("s2"), LogQuery{Source: "panel", From: panel.From, To: now, Level: "info", Limit: 5}, 4}, // without After, always
	} {
		if _, err := s.SystemLog(ctx, c.req, c.q); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if got := viewed(); got != c.want {
			t.Errorf("call %d: audited reads = %d, want %d", i, got, c.want)
		}
	}
}

// TestC360ViewedReadsBounded: parallel first reads of a session give at
// least one audit entry; the noted reads stay bounded, and a full set
// starts again, so a later read is audited once more (D-091).
func TestC360ViewedReadsBounded(t *testing.T) {
	st := storetest.New(t)
	m, err := logmask.New(nil, logmask.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(st, nil)
	s.Use(&hub{code: agentproto.CodeOK, lines: []string{"line"}}, builtin{m})
	id := storetest.CreateUser(t, st, storetest.User{Pseudonym: "a", Role: string(perm.RCONAdmin)})
	req := users.Request{Actor: perm.Actor{ID: id, Role: perm.RCONAdmin}, SessionID: "s"}
	now := time.Now().UTC()
	q := LogQuery{Source: "panel", From: now.Add(-time.Hour), To: now, Level: "info", Limit: 5, After: 1}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, err := s.SystemLog(context.Background(), req, q); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	var n int
	_ = st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE action = 'system_log.viewed'`).Scan(&n)
	if n < 1 || n > 20 {
		t.Errorf("%d audit entries for 20 parallel first reads", n)
	}
	for i := range maxViewed {
		s.markRead(fmt.Sprintf("x%d", i), "panel")
	}
	if len(s.viewed) > maxViewed {
		t.Errorf("%d noted reads, bound %d", len(s.viewed), maxViewed)
	}
	if !s.firstRead("s", "panel", 1) {
		t.Error("a read after the set started again is not audited")
	}
}

// slowBuiltin holds back every value after the first, as a masker whose
// time budget is spent.
type slowBuiltin struct{}

func (slowBuiltin) Lines(lines []string, _ [][]string) ([]string, int) {
	out := make([]string, len(lines))
	for i := range out {
		out[i] = logmask.Withheld
	}
	return out, len(lines)
}

// TestC326WithheldValuesTold (D-091, UI-022): values of the agent log held
// back for the time budget mark the page as not whole and are logged with
// their number, never with a value.
func TestC326WithheldValuesTold(t *testing.T) {
	st := storetest.New(t)
	var logs strings.Builder
	h := &hub{code: agentproto.CodeOK, lines: []string{`{"level":"error","msg":"secret-abc12345"}`}}
	s := NewService(st, slog.New(slog.NewJSONHandler(&logs, nil)))
	s.Use(h, slowBuiltin{})
	admin := users.Request{Actor: perm.Actor{ID: storetest.CreateUser(t, st, storetest.User{Pseudonym: "a", Role: string(perm.RCONAdmin)}), Role: perm.RCONAdmin}}
	p, err := s.SystemLog(context.Background(), admin, LogQuery{Source: "00000000-0000-4000-8000-000000000001", Level: "info", Limit: 50})
	if err != nil || !p.Truncated || len(p.Items) != 1 || strings.Contains(p.Items[0].Text, "abc12345") {
		t.Fatalf("page %+v, %v", p, err)
	}
	if !strings.Contains(logs.String(), `"values":4`) || strings.Contains(logs.String(), "abc12345") {
		t.Errorf("log: %s", logs.String())
	}
}
