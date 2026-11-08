// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/agent"
	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logtest"
	"github.com/NightDragon1/rconductor/internal/rcon/rcontest"
)

// pairAgent pairs an agent with the backend over the agent port, waits
// until it is connected and ties the server to its container mc.
func pairAgent(t *testing.T, admin *client, agentAddr, server string, backend *hostBackend) string {
	t.Helper()
	var tok struct{ Token string }
	admin.must(http.MethodPost, "/agents/tokens", nil, &tok, http.StatusCreated)
	cfg := config.Agent{PairingToken: tok.Token, PanelAddr: agentAddr, SecretsDir: t.TempDir(), Name: "host-1",
		LabelKey: "rconductor.manage", LabelValue: "true", Log: config.Log{Format: "json"}}
	var agentLogs syncBuffer
	a, err := agent.New(cfg, logging.New(&agentLogs, "agent", cfg.Log).Root(), backend)
	if err != nil {
		t.Fatal(err)
	}
	actx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(actx) }()
	t.Cleanup(func() {
		stop()
		if err := <-done; err != nil {
			t.Errorf("agent: %v", err)
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		var containers struct{ Connected bool }
		var list struct {
			Items []struct {
				ID        string `json:"id"`
				Connected bool   `json:"connected"`
			}
		}
		admin.must(http.MethodGet, "/agents", nil, &list, http.StatusOK)
		if len(list.Items) == 1 && list.Items[0].Connected {
			admin.must(http.MethodGet, "/agents/"+list.Items[0].ID+"/containers", nil, &containers, http.StatusOK)
			if containers.Connected {
				admin.must(http.MethodPatch, "/servers/"+server, map[string]string{"agent_id": list.Items[0].ID, "container_name": "mc"}, nil, http.StatusOK)
				return list.Items[0].ID
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("agent not connected:\n%s", agentLogs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// problemOf sends a request as the client and returns status and reason.
func problemOf(t *testing.T, c *client, method, path string, body any) (int, string) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(context.Background(), method, c.base+"/api/v1"+path, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", c.base)
	req.AddCookie(c.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var p struct{ Reason string }
	_ = json.NewDecoder(resp.Body).Decode(&p)
	return resp.StatusCode, p.Reason
}

// TestOlderAgentCapabilities covers AGT-013 Rev. 3 through the stack: the
// panel works with an agent that reports fewer capabilities; the server
// shows them to the admin; an action or a log tail it cannot do is refused
// with 409 agent_unsupported, logged and never sent; its metrics are
// dropped.
func TestOlderAgentCapabilities(t *testing.T) {
	game, err := rcontest.Start("rconpw", rcontest.Minecraft, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(game.Close)
	s := newStack(t, game.Addr, "rconpw")
	admin := s.users["admin"]
	backend := &hostBackend{caps: []string{agentproto.CapRestartInfo}}
	pairAgent(t, admin, s.agentAddr, s.server, backend)

	var srv struct {
		Runtime struct {
			Container struct {
				AgentConnected bool     `json:"agent_connected"`
				Capabilities   []string `json:"capabilities"`
				CPUPercent     *float64 `json:"cpu_percent"`
			}
		}
	}
	admin.must(http.MethodGet, "/servers/"+s.server, nil, &srv, http.StatusOK)
	c := srv.Runtime.Container
	if !c.AgentConnected || !slices.Equal(c.Capabilities, []string{agentproto.CapRestartInfo}) {
		t.Errorf("container = %+v", c)
	}

	for _, who := range []string{"admin", "sa1"} {
		if status, reason := problemOf(t, s.users[who], http.MethodPost, "/servers/"+s.server+"/actions", map[string]string{"action": "restart"}); status != http.StatusConflict || reason != "agent_unsupported" {
			t.Errorf("%s action = %d %q, want 409 agent_unsupported", who, status, reason)
		}
	}
	if status, reason := problemOf(t, admin, http.MethodGet, "/servers/"+s.server+"/logs?lines=10", nil); status != http.StatusConflict || reason != "agent_unsupported" {
		t.Errorf("logs = %d %q, want 409 agent_unsupported", status, reason)
	}
	logtest.WaitCount(t, s.logs, `"reason":"agent_unsupported"`, 3)
	if got := backend.got(); slices.Contains(got, agentproto.TypeContainerAction) || slices.Contains(got, agentproto.TypeLogsTail) {
		t.Errorf("orders sent to an agent without the capability: %v", got)
	}

	// The agent sends metrics all the time; none of them shows.
	for backend.samples.Load() < 5 {
		time.Sleep(20 * time.Millisecond)
	}
	admin.must(http.MethodGet, "/servers/"+s.server, nil, &srv, http.StatusOK)
	if srv.Runtime.Container.CPUPercent != nil {
		t.Errorf("metrics of an agent without the capability shown: %v", *srv.Runtime.Container.CPUPercent)
	}
}
