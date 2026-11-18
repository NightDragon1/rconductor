// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package agentview

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/cache"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// slowMasking masks only once released, as a flood of lines against the
// time budget does.
type slowMasking struct {
	release chan struct{}
	server  string // the server it is slow for; empty: all
}

func (m *slowMasking) ServerLines(ctx context.Context, serverID string, lines []string) []string {
	if m.server == "" || m.server == serverID {
		select {
		case <-m.release:
		case <-ctx.Done():
		}
	}
	out := make([]string, len(lines))
	for i := range lines {
		out[i] = "masked"
	}
	return out
}

type logBus struct{ logs chan Logs }

func (b *logBus) PublishAgentData(_, kind string, data any) {
	if l, ok := data.(Logs); ok && kind == "logs" {
		b.logs <- l
	}
}
func (*logBus) PublishRuntime(string, any, any, any) {}

// TestLogFloodDoesNotHoldAnswers covers UI-022: masking runs apart from
// the read loop of the agent, so a flood of lines that the masking cannot
// keep up with neither holds the next message of the agent nor sends a
// line unmasked; what the queue cannot take is dropped.
func TestLogFloodDoesNotHoldAnswers(t *testing.T) {
	st := storetest.New(t)
	for _, q := range []string{
		`INSERT INTO agents (id, name, public_key, fingerprint, status, created_at) VALUES ('a1', 'host', x'00', 'fp', 'active', 't')`,
		`INSERT INTO servers (id, name, created_at, updated_at, agent_id, container_name) VALUES ('s1', 's1', 't', 't', 'a1', 'mc')`,
	} {
		if _, err := st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	log := slog.New(slog.DiscardHandler)
	bus := &logBus{logs: make(chan Logs, 4*maxMaskQueue)}
	v := New(st, cache.NewAgentData(log), &fakeAgents{}, bus, log)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := &slowMasking{release: make(chan struct{})}
	v.UseMasking(ctx, m)

	const flood = 3 * maxMaskQueue
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range flood {
			v.lines("a1", agentproto.LogLines{Container: "mc", Lines: []string{"password=hunter2x"}}, uint64(i+1), uint64(i+1))
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the deliveries of the agent wait for the masking")
	}
	close(m.release)
	for i := range maxMaskQueue {
		select {
		case l := <-bus.logs:
			if strings.Join(l.Lines, "") != "masked" {
				t.Fatalf("delivery %d: %q", i, l.Lines)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%d of %d deliveries arrived", i, maxMaskQueue)
		}
	}
	cancel()
	if n := len(bus.logs); n > 1 {
		t.Errorf("%d deliveries beyond the queue", n)
	}
}

// TestLogsWithoutMasking covers UI-022: a view that was given no masking
// sends no line as it is (fail closed).
func TestLogsWithoutMasking(t *testing.T) {
	st := storetest.New(t)
	for _, q := range []string{
		`INSERT INTO agents (id, name, public_key, fingerprint, status, created_at) VALUES ('a1', 'host', x'00', 'fp', 'active', 't')`,
		`INSERT INTO servers (id, name, created_at, updated_at, agent_id, container_name) VALUES ('s1', 's1', 't', 't', 'a1', 'mc')`,
	} {
		if _, err := st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	log := slog.New(slog.DiscardHandler)
	bus := &logBus{logs: make(chan Logs, 1)}
	v := New(st, cache.NewAgentData(log), &fakeAgents{}, bus, log)
	v.lines("a1", agentproto.LogLines{Container: "mc", Lines: []string{"password=hunter2x"}}, 1, 1)
	if l := <-bus.logs; strings.Join(l.Lines, "") != withheld {
		t.Errorf("lines without masking: %q", l.Lines)
	}
}

// twoAgents is a view with servers s1 on agent a1 and s2 on agent a2.
func twoAgents(t *testing.T, log *slog.Logger, bus Bus) *View {
	t.Helper()
	st := storetest.New(t)
	for _, q := range []string{
		`INSERT INTO agents (id, name, public_key, fingerprint, status, created_at) VALUES ('a1', 'h1', x'00', 'fp1', 'active', 't'), ('a2', 'h2', x'01', 'fp2', 'active', 't')`,
		`INSERT INTO servers (id, name, created_at, updated_at, agent_id, container_name) VALUES ('s1', 's1', 't', 't', 'a1', 'mc'), ('s2', 's2', 't', 't', 'a2', 'mc')`,
	} {
		if _, err := st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return New(st, cache.NewAgentData(log), &fakeAgents{}, bus, log)
}

// TestLogFloodOfOneAgent covers UI-022: a flood of agent A that the
// masking cannot keep up with drops only deliveries of A; the deliveries
// of agent B all arrive, masked and in order, and none of B is counted.
func TestLogFloodOfOneAgent(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	bus := &logBus{logs: make(chan Logs, 4*maxMaskQueue)}
	v := twoAgents(t, log, bus)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := &slowMasking{release: make(chan struct{}), server: "s1"}
	v.UseMasking(ctx, m)
	const flood = 3 * maxMaskQueue
	for i := range flood {
		v.lines("a1", agentproto.LogLines{Container: "mc", Lines: []string{"x"}}, uint64(i+1), uint64(i+1))
	}
	for i := range maxMaskQueue {
		v.lines("a2", agentproto.LogLines{Container: "mc", Lines: []string{"y"}}, uint64(i+1), uint64(i+1))
	}
	for i := range maxMaskQueue {
		select {
		case l := <-bus.logs:
			if l.ServerID != "s2" || l.Seq != uint64(i+1) || strings.Join(l.Lines, "") != "masked" {
				t.Fatalf("delivery %d of B: %+v", i, l)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%d of %d deliveries of B arrived", i, maxMaskQueue)
		}
	}
	if n := v.droppedDeliveries("a2"); n != 0 {
		t.Errorf("B dropped %d", n)
	}
	if n := v.droppedDeliveries("a1"); n < flood-maxMaskQueue-1 {
		t.Errorf("A dropped %d of %d", n, flood)
	}
	close(m.release)
}

// TestLogDropWarnings covers UI-022: dropped deliveries are warned about
// at most once per agent and minute, with their number.
func TestLogDropWarnings(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	v := twoAgents(t, log, &logBus{logs: make(chan Logs, 4*maxMaskQueue)})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := &slowMasking{release: make(chan struct{})}
	clock := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	v.now = func() time.Time { return clock }
	v.UseMasking(ctx, m)
	flood := func(agent string, n int) {
		for i := range n {
			v.lines(agent, agentproto.LogLines{Container: "mc", Lines: []string{"a", "b"}}, uint64(i+1), uint64(i+1))
		}
	}
	flood("a1", 3*maxMaskQueue)
	flood("a2", 2*maxMaskQueue)
	if n := strings.Count(logs.String(), "log lines dropped"); n != 2 {
		t.Errorf("warnings in the first minute = %d, want one per agent:\n%s", n, logs.String())
	}
	clock = clock.Add(dropWarnEvery)
	logs.Reset()
	flood("a1", 1)
	if !strings.Contains(logs.String(), `"agent_id":"a1","deliveries":`) || strings.Count(logs.String(), "log lines dropped") != 1 {
		t.Errorf("warning after a minute: %s", logs.String())
	}
	close(m.release)
}
