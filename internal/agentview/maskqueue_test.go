// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package agentview

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/cache"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// nAgents is a view with n agents a0… and one server s0… on each.
func nAgents(t *testing.T, n int, log *slog.Logger, bus Bus) *View {
	t.Helper()
	st := storetest.New(t)
	for i := range n {
		for _, q := range []string{
			fmt.Sprintf(`INSERT INTO agents (id, name, public_key, fingerprint, status, created_at) VALUES ('a%d', 'h%d', x'%02x', 'fp%d', 'active', 't')`, i, i, i, i),
			fmt.Sprintf(`INSERT INTO servers (id, name, created_at, updated_at, agent_id, container_name) VALUES ('s%d', 's%d', 't', 't', 'a%d', 'mc')`, i, i, i),
		} {
			if _, err := st.DB.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	}
	return New(st, cache.NewAgentData(log), &fakeAgents{}, bus, log)
}

// lockedBuffer is a log buffer that the workers write while a test reads.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// dropWarnings are the warnings about dropped deliveries in logs.
func dropWarnings(t *testing.T, logs string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for l := range strings.Lines(logs) {
		var e map[string]any
		if json.Unmarshal([]byte(l), &e) == nil && e["msg"] == "log lines dropped: masking is behind" {
			out = append(out, e)
		}
	}
	return out
}

// TestC253QueueFreedWhenIdle covers UI-022: the worker of an agent ends
// and frees its queue after maskIdle without deliveries; drops not yet
// warned about are told then, with the event code (D-077), and the next
// delivery starts a new queue.
func TestC253QueueFreedWhenIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logs lockedBuffer
		log := slog.New(slog.NewJSONHandler(&logs, nil))
		bus := &logBus{logs: make(chan Logs, 4*maxMaskQueue)}
		v := nAgents(t, 1, log, bus)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		m := &slowMasking{release: make(chan struct{}), held: make(chan struct{}, 1)}
		v.UseMasking(ctx, m)
		send := func(seq int) {
			v.lines("a0", agentproto.LogLines{Container: "mc", Lines: []string{"a", "b"}}, uint64(seq), uint64(seq))
		}
		send(1)
		<-m.held
		for i := range maxMaskQueue + 5 { // 64 wait, 5 dropped: 1 warned now, 4 pending
			send(i + 2)
		}
		if w := dropWarnings(t, logs.String()); len(w) != 1 || w[0]["deliveries"] != 1.0 || w[0]["event"] != "logs.lines_dropped" {
			t.Fatalf("first warning: %v", w)
		}
		close(m.release)
		for range maxMaskQueue + 1 {
			<-bus.logs
		}
		time.Sleep(maskIdle - time.Second)
		synctest.Wait()
		v.qmu.Lock()
		_, alive := v.queues["a0"]
		v.qmu.Unlock()
		if !alive || len(dropWarnings(t, logs.String())) != 1 {
			t.Fatalf("before maskIdle: queue alive %t, warnings %v", alive, dropWarnings(t, logs.String()))
		}
		time.Sleep(time.Second)
		synctest.Wait()
		v.qmu.Lock()
		_, alive = v.queues["a0"]
		v.qmu.Unlock()
		w := dropWarnings(t, logs.String())
		if alive || len(w) != 2 || w[1]["deliveries"] != 4.0 || w[1]["lines"] != 8.0 || w[1]["event"] != "logs.lines_dropped" {
			t.Fatalf("after maskIdle: queue alive %t, warnings %v", alive, w)
		}
		send(100)
		if l := <-bus.logs; l.Seq != 100 || strings.Join(l.Lines, "") != "maskedmasked" {
			t.Errorf("delivery after the restart: %+v", l)
		}
		if n := v.droppedDeliveries("a0"); n != 0 {
			t.Errorf("new queue counts %d drops", n)
		}
	})
}

// TestC254ParallelFloods covers UI-022: floods of several agents at once
// arrive in order per agent, and each delivery either arrives once or is
// counted as dropped for its own agent.
func TestC254ParallelFloods(t *testing.T) {
	const agents, each = 8, 3 * maxMaskQueue
	bus := &logBus{logs: make(chan Logs, agents*each)}
	v := nAgents(t, agents, slog.New(slog.DiscardHandler), bus)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := &slowMasking{release: make(chan struct{})}
	close(m.release)
	v.UseMasking(ctx, m)
	var wg sync.WaitGroup
	for a := range agents {
		wg.Go(func() {
			for i := range each {
				v.lines(fmt.Sprintf("a%d", a), agentproto.LogLines{Container: "mc", Lines: []string{"x"}}, uint64(i+1), uint64(i+1))
			}
		})
	}
	wg.Wait()
	want := 0
	dropped := map[string]uint64{}
	for a := range agents {
		dropped[fmt.Sprintf("s%d", a)] = v.droppedDeliveries(fmt.Sprintf("a%d", a))
		want += each - int(dropped[fmt.Sprintf("s%d", a)])
	}
	last, got := map[string]uint64{}, map[string]uint64{}
	for range want {
		select {
		case l := <-bus.logs:
			if l.Seq <= last[l.ServerID] || strings.Join(l.Lines, "") != "masked" {
				t.Fatalf("%s: delivery %d after %d", l.ServerID, l.Seq, last[l.ServerID])
			}
			last[l.ServerID] = l.Seq
			got[l.ServerID]++
		case <-time.After(10 * time.Second):
			t.Fatalf("deliveries so far: %v of %d", got, want)
		}
	}
	for id, n := range got {
		if n+dropped[id] != each {
			t.Errorf("%s: %d arrived, %d dropped, %d sent", id, n, dropped[id], each)
		}
	}
	select {
	case l := <-bus.logs:
		t.Errorf("one delivery too many: %+v", l)
	default:
	}
}
