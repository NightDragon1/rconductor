// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/NightDragon1/rconductor/internal/agent"
	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logtest"
	"github.com/NightDragon1/rconductor/internal/rcon/rcontest"
)

// hostBackend stands in for Docker: one container mc, metrics every 50 ms,
// logs while followed, and every order recorded.
type hostBackend struct {
	mu     sync.Mutex
	orders []string
	// caps replaces the capabilities when set, for an older agent.
	caps []string
	// samples counts the metrics sent.
	samples atomic.Int32
}

func (b *hostBackend) Name() string { return "docker" }
func (b *hostBackend) Capabilities() []string {
	if b.caps != nil {
		return b.caps
	}
	return []string{agentproto.CapContainerAction, agentproto.CapLogs, agentproto.CapMetrics}
}

func (b *hostBackend) Run(ctx context.Context, out agent.Sender) error {
	snap := agentproto.Snapshot{Containers: []agentproto.Container{{ID: "c1", Name: "mc", State: "running", Image: "mc"}}}
	if err := out.Send(ctx, agentproto.TypeSnapshot, "", snap); err != nil {
		return err
	}
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if out.Send(ctx, agentproto.TypeMetrics, "", agentproto.Metrics{Containers: []agentproto.Metric{{Name: "mc", CPUPercent: 42, MemBytes: 1 << 20}}}) == nil {
				b.samples.Add(1)
			}
		}
	}
}

func (b *hostBackend) Handle(ctx context.Context, e agentproto.Envelope, out agent.Sender) {
	b.mu.Lock()
	b.orders = append(b.orders, e.Type)
	b.mu.Unlock()
	if e.Type == agentproto.TypeLogsFollow {
		_ = out.Send(ctx, agentproto.TypeLogLines, e.ID, agentproto.LogLines{Container: "mc", Lines: []string{"[Server] <b>Done</b>"}})
	}
	if e.Type == agentproto.TypeLogsTail {
		// lines=7 stands for a container over the agent's rate.
		if t, _ := agentproto.Payload[agentproto.LogsTail](e); t.Lines == 7 {
			_ = out.Send(ctx, agentproto.TypeResult, e.ID, agentproto.Result{Code: agentproto.CodeRateLimited})
			return
		}
		_ = out.Send(ctx, agentproto.TypeLogLines, e.ID, agentproto.LogLines{Container: "mc", Lines: []string{"old 1", "old 2", "old \x1b[31m3\u202e"}})
	}
	_ = out.Send(ctx, agentproto.TypeResult, e.ID, agentproto.Result{Code: agentproto.CodeOK})
}

func (b *hostBackend) got() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.orders)
}

// next reads WebSocket messages until one matches.
func next(t *testing.T, c *websocket.Conn, match func(typ, topic string, data json.RawMessage) bool) json.RawMessage {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var m struct {
			Type, Topic, Reason string
			Data                json.RawMessage
		}
		if err := readJSON(c, &m); err != nil {
			t.Fatalf("read: %v", err)
		}
		if match(m.Type, m.Topic, m.Data) {
			return m.Data
		}
	}
	t.Fatal("no matching message")
	return nil
}

func subscribe(t *testing.T, c *websocket.Conn, topic string) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"type": "subscribe", "topic": topic})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

// TestAgentThroughTheStack covers the panel side of AGT-003, AGT-008,
// NFR-005 and UI-009 with a real agent and a stand-in backend: pairing by
// token, the server tied to the container, live metrics and logs over the
// WebSocket (logs only while followed), a restart over the agent, and stale
// metrics at once when the agent goes.
func TestAgentThroughTheStack(t *testing.T) {
	game, err := rcontest.Start("rconpw", rcontest.Minecraft, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(game.Close)
	s := newStack(t, game.Addr, "rconpw")
	admin := s.users["admin"]

	var tok struct{ Token string }
	admin.must(http.MethodPost, "/agents/tokens", nil, &tok, http.StatusCreated)
	backend := &hostBackend{}
	cfg := config.Agent{PairingToken: tok.Token, PanelAddr: s.agentAddr, SecretsDir: t.TempDir(), Name: "host-1",
		LabelKey: "rconductor.manage", LabelValue: "true", Log: config.Log{Format: "json"}}
	var agentLogs syncBuffer
	a, err := agent.New(cfg, logging.New(&agentLogs, "agent", cfg.Log).Root(), backend)
	if err != nil {
		t.Fatal(err)
	}
	actx, stopAgent := context.WithCancel(context.Background())
	agentDone := make(chan error, 1)
	go func() { agentDone <- a.Run(actx) }()
	t.Cleanup(func() {
		stopAgent()
		if err := <-agentDone; err != nil {
			t.Errorf("agent: %v", err)
		}
	})

	var agentID string
	deadline := time.Now().Add(10 * time.Second)
	for agentID == "" {
		var list struct {
			Items []struct {
				ID        string `json:"id"`
				Connected bool   `json:"connected"`
			}
		}
		admin.must(http.MethodGet, "/agents", nil, &list, http.StatusOK)
		if len(list.Items) == 1 && list.Items[0].Connected {
			agentID = list.Items[0].ID
		}
		if time.Now().After(deadline) {
			t.Fatalf("agent not connected:\n%s", agentLogs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The server form offers the agent's containers (revision 2.4
	// candidate 22).
	var containers struct {
		Connected bool
		Items     []struct{ Name, Image, State string }
	}
	// The session comes first; the containers follow with the first snapshot.
	for {
		admin.must(http.MethodGet, "/agents/"+agentID+"/containers", nil, &containers, http.StatusOK)
		if containers.Connected || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !containers.Connected || len(containers.Items) != 1 || containers.Items[0].Name != "mc" || containers.Items[0].State != "running" {
		t.Errorf("containers = %+v", containers)
	}
	admin.must(http.MethodPatch, "/servers/"+s.server, map[string]string{"agent_id": agentID, "container_name": "mc"}, nil, http.StatusOK)

	ws := s.console("admin")
	subscribe(t, ws, "server."+s.server+".metrics")
	data := next(t, ws, func(typ, topic string, _ json.RawMessage) bool {
		return typ == "event" && topic == "server."+s.server+".metrics"
	})
	if !strings.Contains(string(data), `"cpu_percent":42`) || !strings.Contains(string(data), `"state":"running"`) {
		t.Errorf("metrics = %s", data)
	}

	// Runtime data in REST and in servers.overview (revision 2.4
	// candidates 3 and 4).
	var srv struct {
		Runtime struct {
			Kind      string
			Container struct {
				Name           string
				State          string
				CPUPercent     float64 `json:"cpu_percent"`
				AgentConnected bool    `json:"agent_connected"`
			}
		}
	}
	admin.must(http.MethodGet, "/servers/"+s.server, nil, &srv, http.StatusOK)
	if srv.Runtime.Kind != "runtime" || srv.Runtime.Container.Name != "mc" || srv.Runtime.Container.CPUPercent != 42 || !srv.Runtime.Container.AgentConnected {
		t.Errorf("runtime = %+v", srv.Runtime)
	}
	subscribe(t, ws, "servers.overview")
	data = next(t, ws, func(typ, topic string, d json.RawMessage) bool {
		return typ == "event" && topic == "servers.overview" && strings.Contains(string(d), `"kind":"runtime"`)
	})
	if !strings.Contains(string(data), `"container":{"name":"mc"`) {
		t.Errorf("overview runtime = %s", data)
	}

	// Logs flow only while someone follows them (UI-009).
	if slices.Contains(backend.got(), agentproto.TypeLogsFollow) {
		t.Error("logs followed before anyone viewed them")
	}
	subscribe(t, ws, "server."+s.server+".logs")
	data = next(t, ws, func(typ, topic string, d json.RawMessage) bool {
		return typ == "event" && topic == "server."+s.server+".logs" && strings.Contains(string(d), "Done")
	})
	if !strings.Contains(string(data), `\u003cb\u003eDone\u003c/b\u003e`) {
		t.Errorf("log lines not escaped JSON: %s", data)
	}
	b, _ := json.Marshal(map[string]string{"type": "unsubscribe", "topic": "server." + s.server + ".logs"})
	_ = ws.Write(context.Background(), websocket.MessageText, b)
	deadline = time.Now().Add(5 * time.Second)
	for !slices.Contains(backend.got(), agentproto.TypeLogsStop) {
		if time.Now().After(deadline) {
			t.Fatalf("no logs.stop after the last viewer left: %v", backend.got())
		}
		time.Sleep(10 * time.Millisecond)
	}
	// UI-009: leaving the page closes the socket; with a second viewer the
	// logs go on, after the last one they end.
	stops := func() int {
		n := 0
		for _, order := range backend.got() {
			if order == agentproto.TypeLogsStop {
				n++
			}
		}
		return n
	}
	before := stops()
	first, second := s.console("admin"), s.console("admin")
	for _, viewer := range []*websocket.Conn{first, second} {
		subscribe(t, viewer, "server."+s.server+".logs")
		// Only the confirmation says that the viewer counts.
		next(t, viewer, func(typ, topic string, _ json.RawMessage) bool {
			return typ == "subscribed" && topic == "server."+s.server+".logs"
		})
	}
	_ = first.CloseNow()
	time.Sleep(300 * time.Millisecond)
	if n := stops(); n != before {
		t.Errorf("logs stopped while a viewer is left: %d, then %d", before, n)
	}
	_ = second.CloseNow()
	for stops() == before {
		if time.Now().After(deadline.Add(5 * time.Second)) {
			t.Fatalf("no logs.stop after the last socket closed: %v", backend.got())
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, ev := range []string{"logs.follow.start", "logs.follow.stop"} {
		// The panel logs each when the agent's result arrives.
		logtest.Wait(t, s.logs, `"event":"`+ev+`"`)
	}

	// Earlier lines come over logs.tail as foreign data, never into the
	// live ring; the agent's rate limit comes through as 429 (revision 2.4
	// candidate 20).
	var tail struct {
		Container string
		Lines     []string
		Truncated bool
	}
	admin.must(http.MethodGet, "/servers/"+s.server+"/logs?lines=2", nil, &tail, http.StatusOK)
	// Control characters go; format characters stay in free text and the
	// interface isolates them (SEC-006).
	if tail.Container != "mc" || !slices.Equal(tail.Lines, []string{"old 2", "old [31m3\u202e"}) || !tail.Truncated {
		t.Errorf("tail = %+v", tail)
	}
	if code := admin.call(http.MethodGet, "/servers/"+s.server+"/logs?lines=7", nil, nil); code != http.StatusTooManyRequests {
		t.Errorf("tail over the agent's rate = %d", code)
	}
	logtest.Wait(t, s.logs, `"event":"logs.tail"`)
	ws2 := s.console("admin")
	subscribe(t, ws2, "server."+s.server+".logs")
	data = next(t, ws2, func(typ, topic string, _ json.RawMessage) bool {
		return typ == "event" && topic == "server."+s.server+".logs"
	})
	if strings.Contains(string(data), "old") {
		t.Errorf("tail lines in the live ring: %s", data)
	}

	// A restart goes to the agent and is audited.
	var res struct{ Action, Via string }
	admin.must(http.MethodPost, "/servers/"+s.server+"/actions", map[string]string{"action": "restart"}, &res, http.StatusOK)
	if res.Via != "agent" || !slices.Contains(backend.got(), agentproto.TypeContainerAction) {
		t.Errorf("action = %+v, orders %v", res, backend.got())
	}
	var auditList struct {
		Items []struct {
			Action string `json:"action"`
			Result string `json:"result"`
		}
	}
	admin.must(http.MethodGet, "/audit?kind=agent", nil, &auditList, http.StatusOK)
	found := false
	for _, e := range auditList.Items {
		found = found || (e.Action == "server.action" && e.Result == "ok")
	}
	if !found {
		t.Errorf("action not in the audit: %+v", auditList.Items)
	}

	// The agent goes: metrics are stale at once, the state unknown.
	stopAgent()
	if err := <-agentDone; err != nil {
		t.Fatalf("agent: %v", err)
	}
	agentDone <- nil
	data = next(t, ws, func(typ, topic string, d json.RawMessage) bool {
		return typ == "event" && topic == "server."+s.server+".metrics" && strings.Contains(string(d), `"stale":true`)
	})
	if !strings.Contains(string(data), `"state":"unknown"`) {
		t.Errorf("metrics after the agent left = %s", data)
	}
	logtest.Wait(t, s.logs, `"event":"agent.disconnected"`, `"event":"cache.stale"`)
	var problem struct{ Reason string }
	if code := admin.call(http.MethodPost, "/servers/"+s.server+"/actions", map[string]string{"action": "start"}, &problem); code != http.StatusServiceUnavailable {
		t.Errorf("action without agent = %d", code)
	}
}
