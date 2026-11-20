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
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
)

// hub answers logs.tail and keeps the order it got.
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

// mask stands for the masking of container logs: it hides abc12345.
type mask struct{}

func (mask) Lines(l []string, _ [][]string) ([]string, int) {
	for i := range l {
		l[i] = strings.ReplaceAll(l[i], "abc12345", "•••")
	}
	return l, 0
}

// TestAgentLog (D-091, UI-025): the own container (@self) over logs.tail,
// masked, from the level up, audited without content; an agent without
// the capability or outside a container is unsupported, one that is not
// connected unavailable.
func TestAgentLog(t *testing.T) {
	st := storetest.New(t)
	h := &hub{code: agentproto.CodeOK, lines: []string{
		`{"level":"info","msg":"paired"}`, `{"level":"error","msg":"docker gone","detail":"token=abc12345"}`, "plain line",
	}}
	s := NewService(st, nil)
	s.Use(h, mask{})
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
