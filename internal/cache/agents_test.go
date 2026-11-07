// SPDX-License-Identifier: AGPL-3.0-or-later

package cache

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logtest"
)

func newAgentData(t *testing.T) (*AgentData, *lockedLog, *time.Time) {
	t.Helper()
	logs := &lockedLog{}
	d := NewAgentData(logging.New(logs, "panel", config.Log{Format: "json"}).Module("cache"))
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return now }
	return d, logs, &now
}

func running(id, name string) agentproto.Container {
	return agentproto.Container{ID: id, Name: name, State: "running", Image: "img"}
}

// TestAgentDataAge covers NFR-005: values carry their age; metrics turn
// stale after 30 s and at once when the agent disconnects; the container
// state is then unknown (cache.stale).
func TestAgentDataAge(t *testing.T) {
	d, logs, now := newAgentData(t)
	var changes []string
	d.OnChange(func(a, c string) { changes = append(changes, a+"/"+c) })
	d.Snapshot("a1", agentproto.Snapshot{Containers: []agentproto.Container{running("1", "mc"), {ID: "2", Name: "\u202eevil", State: "exited"}}})
	if _, ok := d.Container("a1", "evil"); !ok {
		t.Error("container name not cleaned")
	}
	d.Metrics("a1", agentproto.Metrics{Containers: []agentproto.Metric{{Name: "mc", CPUPercent: 12}, {Name: "unknown", CPUPercent: 1}}})
	if m, ok := d.Metric("a1", "mc"); !ok || m.Stale || m.CPUPercent != 12 || !m.At.Equal(*now) {
		t.Errorf("fresh metric = %+v %v", m, ok)
	}
	if _, ok := d.Metric("a1", "unknown"); ok {
		t.Error("metric of a container outside the snapshot kept")
	}
	*now = now.Add(MetricsStale + time.Second)
	if m, _ := d.Metric("a1", "mc"); !m.Stale {
		t.Error("metric not stale after 30 s")
	}
	d.Metrics("a1", agentproto.Metrics{Containers: []agentproto.Metric{{Name: "mc"}}})
	d.Disconnected("a1")
	if m, _ := d.Metric("a1", "mc"); !m.Stale {
		t.Error("metric not stale after the agent left")
	}
	if c, _ := d.Container("a1", "mc"); c.State != StateUnknown {
		t.Errorf("state = %s, want unknown", c.State)
	}
	logtest.Wait(t, logs, `"event":"cache.stale"`, `"reason":"agent_disconnected"`)
	if len(changes) < 4 {
		t.Errorf("changes = %v", changes)
	}
	// A new snapshot brings the agent back.
	d.Snapshot("a1", agentproto.Snapshot{Containers: []agentproto.Container{running("1", "mc")}})
	if c, _ := d.Container("a1", "mc"); c.State != "running" {
		t.Errorf("state after reconnect = %s", c.State)
	}
	if _, ok := d.Container("a1", "evil"); ok {
		t.Error("container gone from the snapshot kept")
	}
}

// TestLogRing: the last 50 lines are kept; a container that starts again
// or is replaced begins with an empty ring (Gesamtkonzept 4.6).
func TestLogRing(t *testing.T) {
	d, _, _ := newAgentData(t)
	d.Snapshot("a1", agentproto.Snapshot{Containers: []agentproto.Container{running("1", "mc")}})
	for i := range 60 {
		d.Lines("a1", agentproto.LogLines{Container: "mc", Lines: []string{fmt.Sprintf("line %d", i)}})
	}
	logs, _, _ := d.Logs("a1", "mc")
	if len(logs) != LogRing || logs[0] != "line 10" || logs[49] != "line 59" {
		t.Errorf("ring = %d lines, %q .. %q", len(logs), logs[0], logs[len(logs)-1])
	}
	d.Event("a1", agentproto.ContainerEvent{Container: agentproto.Container{ID: "1", Name: "mc", State: "exited"}})
	if len(lines(d, "a1", "mc")) != LogRing {
		t.Error("ring cleared on stop")
	}
	d.Event("a1", agentproto.ContainerEvent{Container: running("1", "mc")})
	if len(lines(d, "a1", "mc")) != 0 {
		t.Error("ring kept after restart")
	}
	d.Lines("a1", agentproto.LogLines{Container: "mc", Lines: []string{"x"}})
	d.Event("a1", agentproto.ContainerEvent{Container: running("2", "mc")})
	if len(lines(d, "a1", "mc")) != 0 {
		t.Error("ring kept for a replaced container")
	}
	d.Event("a1", agentproto.ContainerEvent{Container: agentproto.Container{ID: "2", Name: "mc"}, Gone: true})
	if _, ok := d.Container("a1", "mc"); ok {
		t.Error("gone container kept")
	}
}

// TestRestartLoop covers UI-003: three starts within ten minutes mark a
// restart loop; older starts do not count.
func TestRestartLoop(t *testing.T) {
	d, _, now := newAgentData(t)
	start := func(at time.Time) {
		c := running("1", "mc")
		c.StartedAt = at
		d.Event("a1", agentproto.ContainerEvent{Container: c})
	}
	start(now.Add(-20 * time.Minute))
	start(now.Add(-8 * time.Minute))
	start(now.Add(-4 * time.Minute))
	if c, _ := d.Container("a1", "mc"); c.RestartLoop {
		t.Error("two recent starts are a loop")
	}
	start(now.Add(-4 * time.Minute)) // the same start again
	if c, _ := d.Container("a1", "mc"); c.RestartLoop {
		t.Error("a repeated start counted twice")
	}
	start(*now)
	if c, _ := d.Container("a1", "mc"); !c.RestartLoop {
		t.Error("three starts in ten minutes are no loop")
	}
	*now = now.Add(9 * time.Minute)
	if c, _ := d.Container("a1", "mc"); c.RestartLoop {
		t.Error("the loop did not end when the starts got old")
	}
}

// TestAgentDataUTC: every time of the agent data is in UTC (Design 4.1).
func TestAgentDataUTC(t *testing.T) {
	d := NewAgentData(logging.New(&lockedLog{}, "panel", config.Log{Format: "json"}).Module("cache"))
	d.Snapshot("a1", agentproto.Snapshot{Containers: []agentproto.Container{running("1", "mc")}})
	d.Metrics("a1", agentproto.Metrics{Containers: []agentproto.Metric{{Name: "mc"}}})
	if m, _ := d.Metric("a1", "mc"); m.At.Location() != time.UTC {
		t.Errorf("metric time in %v, want UTC", m.At.Location())
	}
}

func lines(d *AgentData, agentID, name string) []string {
	l, _, _ := d.Logs(agentID, name)
	return l
}

// TestLogLineNumbers covers #157: lines keep their numbers over deliveries,
// also those the ring has dropped.
func TestLogLineNumbers(t *testing.T) {
	d, _, _ := newAgentData(t)
	d.Snapshot("a1", agentproto.Snapshot{Containers: []agentproto.Container{running("1", "mc")}})
	var firsts []uint64
	d.OnLines(func(_ string, _ agentproto.LogLines, _, first uint64) { firsts = append(firsts, first) })
	d.Lines("a1", agentproto.LogLines{Container: "mc", Lines: []string{"a", "b"}})
	d.Lines("a1", agentproto.LogLines{Container: "mc", Lines: []string{"c"}})
	if !slices.Equal(firsts, []uint64{1, 3}) {
		t.Errorf("firsts = %v", firsts)
	}
	if _, _, first := d.Logs("a1", "mc"); first != 1 {
		t.Errorf("first = %d", first)
	}
	many := make([]string, LogRing)
	for i := range many {
		many[i] = "x"
	}
	d.Lines("a1", agentproto.LogLines{Container: "mc", Lines: many})
	if lines, _, first := d.Logs("a1", "mc"); len(lines) != LogRing || first != 4 {
		t.Errorf("%d lines, first = %d", len(lines), first)
	}
}

// TestC5NumbersGoOnAfterARecreate: a container recreated under its name goes
// on counting, or clients take its lines for known ones. Failed before the
// fix: seq 1, first 1.
func TestC5NumbersGoOnAfterARecreate(t *testing.T) {
	for name, gone := range map[string]func(d *AgentData){
		"event": func(d *AgentData) {
			d.Event("a", agentproto.ContainerEvent{Container: agentproto.Container{ID: "1", Name: "mc"}, Gone: true})
		},
		"snapshot": func(d *AgentData) { d.Snapshot("a", agentproto.Snapshot{}) },
	} {
		t.Run(name, func(t *testing.T) {
			d, _, _ := newAgentData(t)
			d.Snapshot("a", agentproto.Snapshot{Containers: []agentproto.Container{running("1", "mc")}})
			for range 10 {
				d.Lines("a", agentproto.LogLines{Container: "mc", Lines: []string{"x", "y"}})
			}
			gone(d)
			d.Event("a", agentproto.ContainerEvent{Container: running("2", "mc")})
			var seq, first uint64
			d.OnLines(func(_ string, _ agentproto.LogLines, s, f uint64) { seq, first = s, f })
			d.Lines("a", agentproto.LogLines{Container: "mc", Lines: []string{"new"}})
			if seq != 11 || first != 21 {
				t.Errorf("seq %d, first %d", seq, first)
			}
		})
	}
}
