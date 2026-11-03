// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/NightDragon1/rconductor/internal/agenthub"
	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/connmgr"
	"github.com/NightDragon1/rconductor/internal/users"
)

type fakeAgents struct {
	mu        sync.Mutex
	connected bool
	code      string
	orders    []string
}

func (a *fakeAgents) Request(_ context.Context, agentID, typ string, payload any) (agentproto.Result, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.connected {
		return agentproto.Result{}, agenthub.ErrNotConnected
	}
	act := payload.(agentproto.ContainerAction)
	a.orders = append(a.orders, agentID+" "+typ+" "+act.Container+" "+act.Action)
	return agentproto.Result{Code: a.code}, nil
}

func (a *fakeAgents) RequestData(ctx context.Context, agentID, typ string, payload any, _ func(agentproto.Envelope)) (agentproto.Result, error) {
	return a.Request(ctx, agentID, typ, payload)
}

type fakeRCON struct {
	cmds []string
	err  error
}

func (r *fakeRCON) Execute(_ context.Context, id, cmd string) (string, error) {
	r.cmds = append(r.cmds, id+" "+cmd)
	return "", r.err
}

func (f *fixture) agent(id string) {
	f.t.Helper()
	f.exec(`INSERT INTO agents (id, name, public_key, fingerprint, status, created_at) VALUES (?, 'host', x'00', ?, 'active', 't')`, id, "fp-"+id)
}

func (f *fixture) auditOf(action string) []string {
	f.t.Helper()
	rows, err := f.st.DB.QueryContext(context.Background(),
		`SELECT kind || ' ' || result || ' ' || COALESCE(reason_code, '') || ' ' || payload FROM audit_log WHERE action = ? ORDER BY ts, rowid`, action)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

// TestServerAgentFields covers SRV-001 revision 2: agent and container
// are set together, the agent must be active, the name valid.
func TestServerAgentFields(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.create("mc")
	f.agent("a1")
	agent, name := "a1", "mc-server"
	got, err := f.svc.Update(ctx, f.as("admin"), s.ID, Input{AgentID: &agent, ContainerName: &name})
	if err != nil || got.AgentID != "a1" || got.ContainerName != "mc-server" {
		t.Fatalf("update = %+v %v", got, err)
	}
	empty := ""
	for _, in := range []Input{
		{AgentID: &agent},                            // keeps the container: fine
		{ContainerName: &empty},                      // container without agent
		{AgentID: ptr("nope"), ContainerName: &name}, // unknown agent
		{ContainerName: ptr("mc;rm -rf")},            // invalid name
	} {
		_, err := f.svc.Update(ctx, f.as("admin"), s.ID, in)
		if in.ContainerName == nil {
			if err != nil {
				t.Errorf("%+v: %v", in, err)
			}
			continue
		}
		var inv *users.InvalidError
		if !errors.As(err, &inv) {
			t.Errorf("%+v: %v", in, err)
		}
	}
	got, err = f.svc.Update(ctx, f.as("admin"), s.ID, Input{AgentID: &empty})
	if err != nil || got.AgentID != "" || got.ContainerName != "" {
		t.Errorf("clear = %+v %v", got, err)
	}
	if _, err := f.svc.Update(ctx, f.as("sa"), s.ID, Input{AgentID: &agent, ContainerName: &name}); !errors.Is(err, users.ErrForbidden) {
		t.Errorf("server admin sets the agent: %v", err)
	}
}

func ptr(s string) *string { return &s }

// TestActionsRights covers AGT-008: the RCON admin everywhere, a server
// admin only in his scope, moderators never; every attempt is audited
// (AUD-001).
func TestActionsRights(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	in := f.create("in-scope")
	out := f.create("out-of-scope")
	f.agent("a1")
	for _, s := range []Server{in, out} {
		f.exec(`UPDATE servers SET agent_id = 'a1', container_name = ? WHERE id = ?`, "c-"+s.Name, s.ID)
	}
	f.exec(`INSERT INTO admin_scopes (id, user_id, server_id, assigned_by, created_at) VALUES ('s1', ?, ?, ?, 't')`, f.ids["sa"], in.ID, f.ids["admin"])
	f.c.Invalidate()
	agents := &fakeAgents{connected: true, code: agentproto.CodeOK}
	f.svc.SetActions(agents, &fakeRCON{})

	if r, err := f.svc.Action(ctx, f.as("admin"), out.ID, "restart"); err != nil || r.Via != "agent" {
		t.Errorf("RCON admin: %+v %v", r, err)
	}
	if _, err := f.svc.Action(ctx, f.as("sa"), in.ID, "stop"); err != nil {
		t.Errorf("server admin in scope: %v", err)
	}
	if _, err := f.svc.Action(ctx, f.as("sa"), out.ID, "stop"); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("server admin out of scope: %v", err)
	}
	f.exec(`INSERT INTO command_lists (id, name, owner_id, created_at, updated_at) VALUES ('l', 'l', ?, 't', 't')`, f.ids["admin"])
	f.exec(`INSERT INTO user_groups (id, name, kind, owner_id, command_list_id, created_at, updated_at) VALUES ('mg', 'mg', 'moderator', ?, 'l', 't', 't')`, f.ids["admin"])
	f.exec(`INSERT INTO user_group_members (user_group_id, user_id, added_by, created_at) VALUES ('mg', ?, ?, 't')`, f.ids["m"], f.ids["admin"])
	f.exec(`INSERT INTO grants (id, user_group_id, server_id, granted_by, created_at) VALUES ('g', 'mg', ?, ?, 't')`, in.ID, f.ids["admin"])
	f.c.Invalidate()
	if _, err := f.svc.Action(ctx, f.as("m"), in.ID, "restart"); !errors.Is(err, users.ErrForbidden) {
		t.Errorf("moderator: %v", err)
	}
	if strings.Join(agents.orders, ",") != "a1 container.action c-out-of-scope restart,a1 container.action c-in-scope stop" {
		t.Errorf("orders = %v", agents.orders)
	}
	entries := f.auditOf("server.action")
	if len(entries) != 4 {
		t.Fatalf("audit = %v", entries)
	}
	for i, want := range []string{"agent ok ", "agent ok ", "admin denied not_found", "agent denied forbidden"} {
		if !strings.HasPrefix(entries[i], want) {
			t.Errorf("audit %d = %s, want %s…", i, entries[i], want)
		}
	}
	if _, err := f.svc.Action(ctx, f.as("admin"), in.ID, "remove"); err == nil {
		t.Error("unknown action accepted")
	}
}

// TestActionResults: a disconnected agent is agent_unavailable; the agent's
// codes map to rate limit and failure; everything is audited.
func TestActionResults(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.create("mc")
	f.agent("a1")
	f.exec(`UPDATE servers SET agent_id = 'a1', container_name = 'mc' WHERE id = ?`, s.ID)
	agents := &fakeAgents{}
	f.svc.SetActions(agents, &fakeRCON{})
	if _, err := f.svc.Action(ctx, f.as("admin"), s.ID, "start"); !errors.Is(err, ErrAgentUnavailable) {
		t.Errorf("disconnected: %v", err)
	}
	agents.connected = true
	agents.code = agentproto.CodeRateLimited
	if _, err := f.svc.Action(ctx, f.as("admin"), s.ID, "start"); !errors.Is(err, ErrActionRateLimited) {
		t.Errorf("rate limited: %v", err)
	}
	agents.code = agentproto.CodeForbidden
	var failed *ActionFailedError
	if _, err := f.svc.Action(ctx, f.as("admin"), s.ID, "start"); !errors.As(err, &failed) || failed.Code != agentproto.CodeForbidden {
		t.Errorf("label gone: %v", err)
	}
	got := strings.Join(f.auditOf("server.action"), "\n")
	for _, want := range []string{"error agent_unavailable", "denied rate_limited", "error forbidden"} {
		if !strings.Contains(got, want) {
			t.Errorf("audit without %q:\n%s", want, got)
		}
	}
}

// TestRestartWithoutAgent covers AGT-009: without an agent restart sends the
// profile's stop command over RCON; start and stop need an agent.
func TestRestartWithoutAgent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.create("mc")
	rc := &fakeRCON{}
	f.svc.SetActions(&fakeAgents{}, rc)
	r, err := f.svc.Action(ctx, f.as("admin"), s.ID, "restart")
	if err != nil || r.Via != "rcon" || len(rc.cmds) != 1 || rc.cmds[0] != s.ID+" stop" {
		t.Fatalf("restart = %+v %v %v", r, err, rc.cmds)
	}
	if _, err := f.svc.Action(ctx, f.as("admin"), s.ID, "start"); !errors.Is(err, ErrAgentUnavailable) {
		t.Errorf("start without agent: %v", err)
	}
	rc.err = connmgr.ErrUnavailable
	if _, err := f.svc.Action(ctx, f.as("admin"), s.ID, "restart"); !errors.Is(err, ErrRCONUnavailable) {
		t.Errorf("no RCON: %v", err)
	}
	if got := strings.Join(f.auditOf("server.action"), "\n"); !strings.Contains(got, `"via":"rcon"`) || !strings.Contains(got, `"command":"stop"`) {
		t.Errorf("audit = %s", got)
	}
}
