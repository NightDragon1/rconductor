// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/agent"
	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/rcon/rcontest"
)

// TestO2ReconnectShowsTheCurrentState covers AGT-003 and NFR-005 through
// the stack: a container that started while its agent was away shows
// running once the agent is back, from the snapshot of the new session
// alone, also with the capabilities of an agent 1.0. The panel side passes
// on the code before the fix as well; the agent side of the fix is
// TestO2StateChangeWhileTheAgentStarts.
func TestO2ReconnectShowsTheCurrentState(t *testing.T) {
	for _, tc := range []struct {
		name string
		caps []string
	}{
		{"current", nil},
		{"agent 1.0", []string{agentproto.CapContainerAction, agentproto.CapLogs, agentproto.CapMetrics, agentproto.CapRestartInfo}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			game, err := rcontest.Start("rconpw", rcontest.Minecraft, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(game.Close)
			s := newStack(t, game.Addr, "rconpw")
			admin := s.users["admin"]
			backend := &hostBackend{caps: tc.caps, state: "exited"}
			var tok struct{ Token string }
			admin.must(http.MethodPost, "/agents/tokens", nil, &tok, http.StatusCreated)
			// Both runs share the secrets directory: the same agent comes back.
			cfg := config.Agent{PairingToken: tok.Token, PanelAddr: s.agentAddr, SecretsDir: t.TempDir(), Name: "host-1",
				LabelKey: "rconductor.manage", LabelValue: "true", Log: config.Log{Format: "json"}}
			start := func() func() {
				var logs syncBuffer
				a, err := agent.New(cfg, logging.New(&logs, "agent", cfg.Log).Root(), backend)
				if err != nil {
					t.Fatal(err)
				}
				ctx, stop := context.WithCancel(context.Background())
				done := make(chan error, 1)
				go func() { done <- a.Run(ctx) }()
				return func() {
					stop()
					if err := <-done; err != nil {
						t.Errorf("agent: %v", err)
					}
				}
			}
			state := func() (string, bool) {
				var srv struct {
					Runtime struct {
						Container struct {
							State          string
							AgentConnected bool `json:"agent_connected"`
						}
					}
				}
				admin.must(http.MethodGet, "/servers/"+s.server, nil, &srv, http.StatusOK)
				return srv.Runtime.Container.State, srv.Runtime.Container.AgentConnected
			}
			waitState := func(want string, connected bool) {
				t.Helper()
				deadline := time.Now().Add(10 * time.Second)
				for {
					st, c := state()
					if st == want && c == connected {
						return
					}
					if time.Now().After(deadline) {
						t.Fatalf("state = %q, connected %v; want %q, %v", st, c, want, connected)
					}
					time.Sleep(20 * time.Millisecond)
				}
			}

			stop := start()
			var id string
			for deadline := time.Now().Add(10 * time.Second); id == ""; {
				var list struct {
					Items []struct {
						ID        string `json:"id"`
						Connected bool   `json:"connected"`
					}
				}
				admin.must(http.MethodGet, "/agents", nil, &list, http.StatusOK)
				if len(list.Items) == 1 && list.Items[0].Connected {
					id = list.Items[0].ID
				}
				if time.Now().After(deadline) {
					t.Fatal("agent not connected")
				}
				time.Sleep(20 * time.Millisecond)
			}
			admin.must(http.MethodPatch, "/servers/"+s.server, map[string]string{"agent_id": id, "container_name": "mc"}, nil, http.StatusOK)
			waitState("exited", true)

			stop()
			waitState("unknown", false)
			// The container starts while the agent is away: no event reaches
			// the panel.
			backend.mu.Lock()
			backend.state = "running"
			backend.mu.Unlock()
			stop = start()
			defer stop()
			waitState("running", true)
		})
	}
}
