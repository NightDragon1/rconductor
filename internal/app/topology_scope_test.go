// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// overviewOf opens a WebSocket of role and subscribes servers.overview; it
// returns once the subscription is confirmed.
func overviewOf(t *testing.T, f *rightsFixture, role string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(f.base, "http")+"/api/v1/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {f.base}, "Cookie": {f.c[role].cookie.String()}},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	subscribe(t, c, "servers.overview")
	for {
		var m struct{ Type, Topic string }
		if err := readJSON(c, &m); err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if m.Topic == "servers.overview" {
			if m.Type != "subscribed" {
				t.Fatalf("%s: servers.overview refused: %s", role, m.Type)
			}
			return c
		}
	}
}

// overviewSeen is what one viewer got in servers.overview: runtime events
// per server, whether they had a container part, and the raw messages.
type overviewSeen struct {
	runtime   map[string]int
	container map[string]int
	raw       []string
	err       error
}

// readOverview reads servers.overview until enough runtime events of each
// server in want have come, or the deadline passes.
func readOverview(c *websocket.Conn, want map[string]int, deadline time.Time) overviewSeen {
	seen := overviewSeen{runtime: map[string]int{}, container: map[string]int{}}
	done := func() bool {
		for id, n := range want {
			if seen.runtime[id] < n {
				return false
			}
		}
		return true
	}
	for !done() {
		if time.Now().After(deadline) {
			seen.err = context.DeadlineExceeded
			return seen
		}
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		_, data, err := c.Read(ctx)
		cancel()
		if err != nil {
			seen.err = err
			return seen
		}
		seen.raw = append(seen.raw, string(data))
		var m struct {
			Type, Topic string
			Data        struct {
				Kind      string          `json:"kind"`
				ServerID  string          `json:"server_id"`
				Container json.RawMessage `json:"container"`
			}
		}
		if json.Unmarshal(data, &m) != nil || m.Type != "event" || m.Topic != "servers.overview" || m.Data.Kind != "runtime" {
			continue
		}
		seen.runtime[m.Data.ServerID]++
		if len(m.Data.Container) > 0 && string(m.Data.Container) != "null" {
			seen.container[m.Data.ServerID]++
		}
	}
	return seen
}

// TestTopologyScopeUI010 covers UI-010 with UI-006 over WebSocket: the
// live data of the graph reaches a server admin only for servers in his
// scope, also for updates that start after he subscribed; moderator and
// supporter get runtime data of their server without any agent part and
// nothing of other servers.
func TestTopologyScopeUI010(t *testing.T) {
	f := newRightsFixture(t)
	viewers := []string{rA, rSI, rSO, rMR, rMN, rSU}
	conns := map[string]*websocket.Conn{}
	for _, v := range viewers {
		conns[v] = overviewOf(t, f, v)
	}
	// Both servers get a container only now, after every viewer subscribed:
	// each runtime event below is an update sent after subscribing.
	agentID := pairAgent(t, f.as(rA), f.agentAddr, f.server2, &hostBackend{})
	f.as(rA).must(http.MethodPatch, "/servers/"+f.server, map[string]string{"agent_id": agentID, "container_name": "mc"}, nil, http.StatusOK)

	// want: the runtime events each viewer must see; in scope only.
	const n = 5
	want := map[string]map[string]int{
		rA:  {f.server: n, f.server2: n},
		rSI: {f.server: n},
		rSO: {f.server2: n},
		rMR: {f.server: n},
		rMN: {f.server: n},
		rSU: {f.server: n},
	}
	results := map[string]overviewSeen{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	deadline := time.Now().Add(15 * time.Second)
	for _, v := range viewers {
		wg.Go(func() {
			s := readOverview(conns[v], want[v], deadline)
			mu.Lock()
			results[v] = s
			mu.Unlock()
		})
	}
	wg.Wait()

	outside := map[string]string{rSI: f.server2, rSO: f.server, rMR: f.server2, rMN: f.server2, rSU: f.server2}
	for _, v := range viewers {
		s := results[v]
		if s.err != nil {
			t.Errorf("%s: %v after %v", v, s.err, s.runtime)
			continue
		}
		if id, ok := outside[v]; ok {
			for _, raw := range s.raw {
				if strings.Contains(raw, id) {
					t.Errorf("%s got a message of a server outside his scope: %s", v, raw)
				}
			}
		}
		for _, raw := range s.raw {
			// The agent's name is for the RCON admin only (F10).
			if v != rA && strings.Contains(raw, "host-1") {
				t.Errorf("%s got the agent's name: %s", v, raw)
			}
		}
		switch v {
		case rA, rSI, rSO:
			// The graph follows the agent state of every server in scope.
			for id := range want[v] {
				if s.container[id] == 0 {
					t.Errorf("%s: no container part for %s in %d runtime events", v, id, s.runtime[id])
				}
			}
		default:
			if len(s.container) > 0 {
				t.Errorf("%s got agent data: %v", v, s.container)
			}
			for _, raw := range s.raw {
				if strings.Contains(raw, "agent_connected") || strings.Contains(raw, `"container"`) {
					t.Errorf("%s got agent data: %s", v, raw)
				}
			}
		}
	}
}

// TestTopologyScopeRESTUI010 covers UI-010 with UI-006 over REST: the
// server list, the server groups and the server itself show a server
// admin only the servers in his scope, with their agent state but no
// agent; moderator and supporter read no agent and no group and get their
// servers without any agent part.
func TestTopologyScopeRESTUI010(t *testing.T) {
	f := newRightsFixture(t)
	agentID := pairAgent(t, f.as(rA), f.agentAddr, f.server2, &hostBackend{})
	f.as(rA).must(http.MethodPatch, "/servers/"+f.server, map[string]string{"agent_id": agentID, "container_name": "mc"}, nil, http.StatusOK)
	// A group with both servers, as the graph groups by server groups.
	f.as(rA).must(http.MethodPost, "/server-groups", map[string]any{"name": "group-both", "server_ids": []string{f.server, f.server2}}, nil, http.StatusCreated)

	listed := func(role string) ([]string, string) {
		status, b := f.rawBody(role, http.MethodGet, "/servers", nil)
		if status != http.StatusOK {
			t.Fatalf("%s: GET /servers = %d", role, status)
		}
		var l struct{ Items []struct{ ID string } }
		if err := json.Unmarshal(b, &l); err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, it := range l.Items {
			ids = append(ids, it.ID)
		}
		return ids, string(b)
	}

	// The RCON admin sees both, with the agent state.
	if ids, body := listed(rA); !slices.Contains(ids, f.server) || !slices.Contains(ids, f.server2) || !strings.Contains(body, `"agent_connected":true`) {
		t.Errorf("RCON admin: %v %s", ids, body)
	}

	for role, pair := range map[string][2]string{rSI: {f.server, f.server2}, rSO: {f.server2, f.server}} {
		in, out := pair[0], pair[1]
		ids, body := listed(role)
		if !slices.Equal(ids, []string{in}) || strings.Contains(body, out) {
			t.Errorf("%s: servers %v, want only %s: %s", role, ids, in, body)
		}
		if !strings.Contains(body, `"agent_connected":true`) || strings.Contains(body, "host-1") {
			t.Errorf("%s: want the agent state without the agent's name: %s", role, body)
		}
		if status, _ := f.raw(role, http.MethodGet, "/servers/"+out, nil); status != http.StatusNotFound {
			t.Errorf("%s: server outside the scope = %d, want 404", role, status)
		}
		status, groups := f.rawBody(role, http.MethodGet, "/server-groups", nil)
		if status != http.StatusOK || strings.Contains(string(groups), out) {
			t.Errorf("%s: server groups %d name a server outside the scope: %s", role, status, groups)
		}
		if status, _ := f.raw(role, http.MethodGet, "/agents", nil); status != http.StatusForbidden {
			t.Errorf("%s reads the agents: %d", role, status)
		}
	}

	for _, role := range []string{rMR, rMN, rSU} {
		ids, body := listed(role)
		if !slices.Equal(ids, []string{f.server}) || strings.Contains(body, f.server2) {
			t.Errorf("%s: servers %v, want only %s", role, ids, f.server)
		}
		_, one := f.rawBody(role, http.MethodGet, "/servers/"+f.server, nil)
		for _, b := range []string{body, string(one)} {
			for _, leak := range []string{`"container"`, "agent_connected", agentID, "host-1", `"mc"`} {
				if strings.Contains(b, leak) {
					t.Errorf("%s: agent data %s in %s", role, leak, b)
				}
			}
		}
		for _, path := range []string{"/agents", "/agents/" + agentID, "/agents/" + agentID + "/containers", "/server-groups"} {
			if status, _ := f.raw(role, http.MethodGet, path, nil); status != http.StatusForbidden {
				t.Errorf("%s: GET %s = %d, want 403", role, path, status)
			}
		}
	}
}
