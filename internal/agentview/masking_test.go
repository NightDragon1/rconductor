// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package agentview

import (
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
type slowMasking struct{ release chan struct{} }

func (m *slowMasking) ServerLines(ctx context.Context, _ string, lines []string) []string {
	select {
	case <-m.release:
	case <-ctx.Done():
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
