// SPDX-License-Identifier: AGPL-3.0-or-later

package cache

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
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
	if !strings.Contains(logs.String(), `"event":"cache.stale"`) || !strings.Contains(logs.String(), `"reason":"agent_disconnected"`) {
		t.Error("cache.stale missing")
	}
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
	logs := d.Logs("a1", "mc")
	if len(logs) != LogRing || logs[0] != "line 10" || logs[49] != "line 59" {
		t.Errorf("ring = %d lines, %q .. %q", len(logs), logs[0], logs[len(logs)-1])
	}
	d.Event("a1", agentproto.ContainerEvent{Container: agentproto.Container{ID: "1", Name: "mc", State: "exited"}})
	if len(d.Logs("a1", "mc")) != LogRing {
		t.Error("ring cleared on stop")
	}
	d.Event("a1", agentproto.ContainerEvent{Container: running("1", "mc")})
	if len(d.Logs("a1", "mc")) != 0 {
		t.Error("ring kept after restart")
	}
	d.Lines("a1", agentproto.LogLines{Container: "mc", Lines: []string{"x"}})
	d.Event("a1", agentproto.ContainerEvent{Container: running("2", "mc")})
	if len(d.Logs("a1", "mc")) != 0 {
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
