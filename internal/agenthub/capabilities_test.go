// SPDX-License-Identifier: AGPL-3.0-or-later

package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/logtest"
)

// write sends one message of the agent.
func write(t *testing.T, c *websocket.Conn, typ, ref string, payload any) {
	t.Helper()
	data, _ := agentproto.Encode(typ, ref, payload)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

// answerNext reads the next order at the agent, answers it with ok and
// returns its type.
func answerNext(t *testing.T, c *websocket.Conn) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("no order at the agent: %v", err)
	}
	var e agentproto.Envelope
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatal(err)
	}
	write(t, c, agentproto.TypeResult, e.ID, agentproto.Result{Code: agentproto.CodeOK})
	return e.Type
}

// TestCapabilities covers AGT-013 Rev. 3 at the hub: an order the agent
// reports no capability for is refused before it is sent, also for many
// at once; orders every agent handles still go out; metrics of an agent
// without the capability are dropped; the capabilities of the last hello
// stay known after the session and follow a reconnect.
func TestCapabilities(t *testing.T) {
	f := newFixture(t, ThrottleOptions{})
	agent := agentKey(t)
	f.known.keys[agent.Fingerprint()] = "agent-1"
	got := make(chan string, 8)
	f.hub.OnMessage(func(id string, e agentproto.Envelope) { got <- e.Type })
	gone := make(chan string, 4)
	f.hub.OnDisconnect(func(id string) { gone <- id })
	if _, ok := f.hub.Capabilities("agent-1"); ok {
		t.Error("capabilities known before the agent connected")
	}

	c, err := f.dial(t, agent, f.panel.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	hello(t, c, agentproto.Hello{AgentID: "agent-1", Capabilities: []string{"version.detect"}})
	logtest.Wait(t, f.logs, `"event":"agent.connected"`)
	if caps, ok := f.hub.Capabilities("agent-1"); !ok || !slices.Equal(caps, []string{"version.detect"}) {
		t.Errorf("capabilities = %v %v", caps, ok)
	}

	ctx := context.Background()
	if _, err := f.hub.Request(ctx, "agent-1", agentproto.TypeContainerAction,
		agentproto.ContainerAction{Container: "mc", Action: agentproto.ActionStop}); !errors.Is(err, ErrNotCapable) {
		t.Errorf("container.action without capability: %v", err)
	}
	if _, err := f.hub.RequestData(ctx, "agent-1", agentproto.TypeLogsTail,
		agentproto.LogsTail{Container: "mc", Lines: 10}, func(agentproto.Envelope) {}); !errors.Is(err, ErrNotCapable) {
		t.Errorf("logs.tail without capability: %v", err)
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if _, err := f.hub.Request(ctx, "agent-1", agentproto.TypeLogsFollow,
				agentproto.LogsFollow{Container: "mc"}); !errors.Is(err, ErrNotCapable) {
				t.Errorf("parallel logs.follow without capability: %v", err)
			}
		})
	}
	wg.Wait()
	// logs.stop needs no capability: it is the first order the agent sees.
	res := make(chan error, 1)
	go func() {
		_, err := f.hub.Request(ctx, "agent-1", agentproto.TypeLogsStop, agentproto.LogsFollow{Container: "mc"})
		res <- err
	}()
	if typ := answerNext(t, c); typ != agentproto.TypeLogsStop {
		t.Errorf("the agent got %s first, want logs.stop", typ)
	}
	if err := <-res; err != nil {
		t.Errorf("logs.stop: %v", err)
	}

	// Metrics without the capability are dropped, the snapshot after them
	// arrives.
	write(t, c, agentproto.TypeMetrics, "", agentproto.Metrics{Containers: []agentproto.Metric{{Name: "mc"}}})
	write(t, c, agentproto.TypeSnapshot, "", agentproto.Snapshot{})
	if typ := next(t, got); typ != agentproto.TypeSnapshot {
		t.Errorf("first message = %s, want the snapshot: metrics must be dropped", typ)
	}

	_ = c.Close(websocket.StatusNormalClosure, "bye")
	<-gone
	if caps, ok := f.hub.Capabilities("agent-1"); !ok || !slices.Equal(caps, []string{"version.detect"}) {
		t.Errorf("capabilities after the session = %v %v", caps, ok)
	}
	if _, err := f.hub.Request(ctx, "agent-1", agentproto.TypeContainerAction,
		agentproto.ContainerAction{Container: "mc", Action: agentproto.ActionStop}); !errors.Is(err, ErrNotConnected) {
		t.Errorf("without session: %v", err)
	}

	// After an update the agent reports more; the new hello counts.
	c2, err := f.dial(t, agent, f.panel.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	hello(t, c2, agentproto.Hello{AgentID: "agent-1", Capabilities: []string{agentproto.CapContainerAction, agentproto.CapMetrics}})
	// The hub notes the capabilities before its welcome.
	if caps, _ := f.hub.Capabilities("agent-1"); !slices.Contains(caps, agentproto.CapMetrics) {
		t.Errorf("capabilities after the reconnect = %v", caps)
	}
	go func() {
		_, err := f.hub.Request(ctx, "agent-1", agentproto.TypeContainerAction,
			agentproto.ContainerAction{Container: "mc", Action: agentproto.ActionStop})
		res <- err
	}()
	if typ := answerNext(t, c2); typ != agentproto.TypeContainerAction {
		t.Errorf("the agent got %s, want container.action", typ)
	}
	if err := <-res; err != nil {
		t.Errorf("container.action with capability: %v", err)
	}
	write(t, c2, agentproto.TypeMetrics, "", agentproto.Metrics{Containers: []agentproto.Metric{{Name: "mc"}}})
	if typ := next(t, got); typ != agentproto.TypeMetrics {
		t.Errorf("message with capability = %s, want metrics", typ)
	}
}

// next waits up to 5 s for the next message the hub hands on.
func next(t *testing.T, got <-chan string) string {
	t.Helper()
	select {
	case typ := <-got:
		return typ
	case <-time.After(5 * time.Second):
		t.Fatal("no message handed on within 5 s")
		return ""
	}
}
