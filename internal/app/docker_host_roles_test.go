// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/NightDragon1/rconductor/internal/agentproto"
)

// wsRecorder keeps every message one role gets on its WebSocket.
type wsRecorder struct {
	mu  sync.Mutex
	raw []string
}

func (r *wsRecorder) add(m string) { r.mu.Lock(); r.raw = append(r.raw, m); r.mu.Unlock() }

// runtimeSince counts the runtime events from message n on.
func (r *wsRecorder) runtimeSince(n int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := 0
	for _, m := range r.raw[min(n, len(r.raw)):] {
		if strings.Contains(m, `"kind":"runtime"`) {
			c++
		}
	}
	return c
}

func (r *wsRecorder) size() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.raw) }

func (r *wsRecorder) all() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.raw, "\n")
}

// recordTopics opens a WebSocket of role, subscribes to every topic the
// panel knows for the servers and records what comes until the test ends.
// It returns nil for a role that gets no WebSocket.
func recordTopics(t *testing.T, f *rightsFixture, role string, servers ...string) *wsRecorder {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	dctx, dcancel := context.WithTimeout(ctx, 5*time.Second)
	defer dcancel()
	c, resp, err := websocket.Dial(dctx, "ws"+strings.TrimPrefix(f.base, "http")+"/api/v1/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {f.base}, "Cookie": {f.c[role].cookie.String()}},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		cancel()
		return nil
	}
	topics := []string{"servers.overview", "user.rights"}
	for _, s := range servers {
		for _, k := range []string{"status", "console", "metrics", "logs"} {
			topics = append(topics, "server."+s+"."+k)
		}
	}
	for _, topic := range topics {
		subscribe(t, c, topic)
	}
	r := &wsRecorder{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			r.add(string(data))
		}
	}()
	t.Cleanup(func() { cancel(); _ = c.CloseNow(); <-done })
	return r
}

// TestDockerHostOnlyRCONAdmin covers AGT-014 and D-084 with a real agent and
// hub session: with the flag docker_host on, the Docker host and the
// address the agent connected from reach the RCON admin in the agent data, and no other role in any
// answer over HTTP or in any WebSocket topic.
func TestDockerHostOnlyRCONAdmin(t *testing.T) {
	f := newRightsFixture(t)
	const name, ip, remote = "nas-7f3", "198.51.100.7", "203.0.113.9"
	f.as(rA).must(http.MethodPatch, "/settings", map[string]any{"features.docker_host": true}, nil, http.StatusOK)
	others := []string{rSI, rSO, rMR, rMN, rSU, rP}
	rec := map[string]*wsRecorder{}
	for _, role := range append([]string{rA}, others...) {
		rec[role] = recordTopics(t, f, role, f.server, f.server2)
	}
	backend := &hostBackend{host: &agentproto.DockerHost{Name: name, IP: ip}}
	agentID := pairAgent(t, f.as(rA), f.agentAddr, f.server, backend)
	if _, err := f.db().ExecContext(context.Background(), `UPDATE agents SET remote = ? WHERE id = ?`, remote, agentID); err != nil {
		t.Fatal(err)
	}

	// Live data of the agent's server must reach every viewer in scope
	// after the address is set, so a topic carrying it would show it. No
	// topic carries agent data today, so there is nothing to find for the
	// RCON admin either. The wait polls a real agent and hub.
	since := map[string]int{}
	for role, r := range rec {
		if r != nil {
			since[role] = r.size()
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	for _, role := range []string{rA, rSI, rMR, rMN, rSU} {
		for rec[role] != nil && rec[role].runtimeSince(since[role]) < 2 {
			if time.Now().After(deadline) {
				t.Fatalf("%s: no runtime event in servers.overview", role)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	for _, p := range []string{"/agents", "/agents/" + agentID} {
		status, body := f.rawBody(rA, http.MethodGet, p, nil)
		for _, want := range []string{name, ip, remote} {
			if status != http.StatusOK || !strings.Contains(string(body), want) {
				t.Errorf("RCON admin GET %s = %d, %s missing", p, status, want)
			}
		}
	}
	paths := []string{"/agents", "/agents/" + agentID, "/agents/" + agentID + "/containers", "/servers", "/servers/" + f.server,
		"/servers/" + f.server + "/runtime", "/server-groups", "/overview", "/about", "/me"}
	for _, role := range append(others, rX) {
		for _, p := range paths {
			status, body := f.rawBody(role, http.MethodGet, p, nil)
			for _, leak := range []string{name, ip, remote} {
				if strings.Contains(string(body), leak) {
					t.Errorf("%s GET %s = %d carries %s", role, p, status, leak)
				}
			}
		}
		if r := rec[role]; r != nil {
			for _, leak := range []string{name, ip, remote} {
				if strings.Contains(r.all(), leak) {
					t.Errorf("%s gets %s over the WebSocket", role, leak)
				}
			}
		}
	}
}
