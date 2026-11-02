// SPDX-License-Identifier: AGPL-3.0-or-later

package cache

import (
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/logging"
)

// Timing of agent data (Gesamtkonzept 4.6).
const (
	MetricsStale = 30 * time.Second
	LogRing      = 50
	// A container that started LoopStarts times within LoopWindow is in a
	// restart loop (UI-003, revision 2.4 candidate 2).
	LoopStarts = 3
	LoopWindow = 10 * time.Minute
)

// StateUnknown is the container state while its agent is disconnected.
const StateUnknown = "unknown"

// ContainerView is a container as the panel shows it, with its age.
type ContainerView struct {
	agentproto.Container
	UpdatedAt   time.Time
	RestartLoop bool
}

// MetricView is the last sample of a container; Stale is set after 30 s,
// when the agent is gone or the container does not run (NFR-005).
type MetricView struct {
	agentproto.Metric
	At    time.Time
	Stale bool
}

type containerEntry struct {
	c        agentproto.Container
	updated  time.Time
	metric   *agentproto.Metric
	metricAt time.Time
	logs     []string
	starts   []time.Time // distinct start times within LoopWindow
}

// started notes the start time of the container, if new.
func (e *containerEntry) started(t time.Time) {
	if t.IsZero() || (len(e.starts) > 0 && e.starts[len(e.starts)-1].Equal(t)) {
		return
	}
	e.starts = append(e.starts, t)
	if len(e.starts) > 16 {
		e.starts = slices.Clone(e.starts[len(e.starts)-16:])
	}
}

func (e *containerEntry) inLoop(now time.Time) bool {
	n := 0
	for _, t := range e.starts {
		if now.Sub(t) <= LoopWindow {
			n++
		}
	}
	return n >= LoopStarts
}

type agentEntry struct {
	connected  bool
	containers map[string]*containerEntry
}

// AgentData holds what agents report, in memory only (NFR-005).
type AgentData struct {
	log *slog.Logger
	now func() time.Time

	mu     sync.Mutex
	agents map[string]*agentEntry
	// Receivers for the event bus; set before data arrives.
	onChange  func(agentID, container string)
	onMetrics func(agentID string, containers []string)
	onLines   func(agentID string, ll agentproto.LogLines)
	onConnect func(agentID string)
}

// OnMetrics sets the receiver of new samples.
func (d *AgentData) OnMetrics(f func(agentID string, containers []string)) { d.onMetrics = f }

// OnLines sets the receiver of new log lines.
func (d *AgentData) OnLines(f func(agentID string, ll agentproto.LogLines)) { d.onLines = f }

// OnConnect sets the receiver of the first snapshot after a connection.
func (d *AgentData) OnConnect(f func(agentID string)) { d.onConnect = f }

// NewAgentData returns an empty store.
func NewAgentData(log *slog.Logger) *AgentData {
	return &AgentData{log: log, now: time.Now, agents: map[string]*agentEntry{}}
}

// OnChange sets the receiver of container changes.
func (d *AgentData) OnChange(f func(agentID, container string)) { d.onChange = f }

func (d *AgentData) agent(id string) *agentEntry {
	a := d.agents[id]
	if a == nil {
		a = &agentEntry{containers: map[string]*containerEntry{}}
		d.agents[id] = a
	}
	return a
}

func (d *AgentData) changed(agentID string, names ...string) {
	if d.onChange == nil {
		return
	}
	for _, n := range names {
		d.onChange(agentID, n)
	}
}

// Snapshot replaces the containers of an agent; it also marks the agent
// connected.
func (d *AgentData) Snapshot(agentID string, s agentproto.Snapshot) {
	d.mu.Lock()
	a := d.agent(agentID)
	fresh := !a.connected
	a.connected = true
	now := d.now()
	seen := map[string]bool{}
	var names []string
	for i, c := range s.Containers {
		if i == agentproto.MaxContainers {
			break
		}
		c.Clean()
		if c.Name == "" {
			continue
		}
		seen[c.Name] = true
		e := a.containers[c.Name]
		if e == nil {
			e = &containerEntry{}
			a.containers[c.Name] = e
		}
		if e.c.ID != "" && e.c.ID != c.ID {
			e.logs, e.metric = nil, nil // another container under this name
		}
		e.c, e.updated = c, now
		e.started(c.StartedAt)
		names = append(names, c.Name)
	}
	for n := range a.containers {
		if !seen[n] {
			delete(a.containers, n)
			names = append(names, n)
		}
	}
	d.mu.Unlock()
	d.changed(agentID, names...)
	if fresh && d.onConnect != nil {
		d.onConnect(agentID)
	}
}

// Event applies a status change (Gesamtkonzept 4.6: at once).
func (d *AgentData) Event(agentID string, ev agentproto.ContainerEvent) {
	c := ev.Container
	c.Clean()
	if c.Name == "" {
		return
	}
	d.mu.Lock()
	a := d.agent(agentID)
	if ev.Gone {
		delete(a.containers, c.Name)
	} else {
		e := a.containers[c.Name]
		if e == nil {
			e = &containerEntry{}
			a.containers[c.Name] = e
		}
		// A container that starts again or was replaced begins with an
		// empty log ring (Gesamtkonzept 4.6).
		if (e.c.ID != "" && e.c.ID != c.ID) || (e.c.State != "running" && c.State == "running") {
			e.logs = nil
		}
		e.c, e.updated = c, d.now()
		e.started(c.StartedAt)
	}
	d.mu.Unlock()
	d.changed(agentID, c.Name)
}

// Metrics stores a sample for every container in it.
func (d *AgentData) Metrics(agentID string, m agentproto.Metrics) {
	d.mu.Lock()
	a := d.agent(agentID)
	now := d.now()
	var names []string
	for i, s := range m.Containers {
		if i == agentproto.MaxContainers {
			break
		}
		e := a.containers[s.Name]
		if e == nil {
			continue // not in the snapshot: nothing to show it under
		}
		s := s
		e.metric, e.metricAt = &s, now
		names = append(names, s.Name)
	}
	d.mu.Unlock()
	if d.onMetrics != nil && len(names) > 0 {
		d.onMetrics(agentID, names)
	}
}

// Lines appends log lines to the ring of the container.
func (d *AgentData) Lines(agentID string, ll agentproto.LogLines) {
	lines, cut := agentproto.LimitLines(ll.Lines)
	d.mu.Lock()
	e := d.agent(agentID).containers[ll.Container]
	if e == nil {
		d.mu.Unlock()
		return
	}
	e.logs = append(e.logs, lines...)
	if len(e.logs) > LogRing {
		e.logs = slices.Clone(e.logs[len(e.logs)-LogRing:])
	}
	d.mu.Unlock()
	if d.onLines != nil && len(lines) > 0 {
		d.onLines(agentID, agentproto.LogLines{Container: ll.Container, Lines: lines, Truncated: cut || ll.Truncated})
	}
}

// Disconnected marks the containers of an agent unknown and their metrics
// stale at once (NFR-005).
func (d *AgentData) Disconnected(agentID string) {
	d.mu.Lock()
	a := d.agents[agentID]
	if a == nil || !a.connected {
		d.mu.Unlock()
		return
	}
	a.connected = false
	names := make([]string, 0, len(a.containers))
	for n := range a.containers {
		names = append(names, n)
	}
	d.mu.Unlock()
	d.log.Info("agent data is stale", logging.Event(logging.CacheStale), slog.String("agent_id", agentID),
		slog.String("data", "containers"), slog.String(logging.KeyReason, "agent_disconnected"))
	d.changed(agentID, names...)
}

// Receive takes a message of an agent from the hub; payloads that do not
// decode are dropped.
func (d *AgentData) Receive(agentID string, e agentproto.Envelope) {
	switch e.Type {
	case agentproto.TypeSnapshot:
		if s, err := agentproto.Payload[agentproto.Snapshot](e); err == nil {
			d.Snapshot(agentID, s)
		}
	case agentproto.TypeContainerEvent:
		if ev, err := agentproto.Payload[agentproto.ContainerEvent](e); err == nil {
			d.Event(agentID, ev)
		}
	case agentproto.TypeMetrics:
		if m, err := agentproto.Payload[agentproto.Metrics](e); err == nil {
			d.Metrics(agentID, m)
		}
	case agentproto.TypeLogLines:
		if ll, err := agentproto.Payload[agentproto.LogLines](e); err == nil {
			d.Lines(agentID, ll)
		}
	}
}

// Container returns a container; its state is unknown while the agent is
// disconnected.
func (d *AgentData) Container(agentID, name string) (ContainerView, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	a := d.agents[agentID]
	if a == nil {
		return ContainerView{}, false
	}
	e := a.containers[name]
	if e == nil {
		return ContainerView{}, false
	}
	v := ContainerView{Container: e.c, UpdatedAt: e.updated, RestartLoop: e.inLoop(d.now())}
	v.Ports, v.Volumes = slices.Clone(e.c.Ports), slices.Clone(e.c.Volumes)
	if !a.connected {
		v.State = StateUnknown
	}
	return v, true
}

// Metric returns the last sample of a container.
func (d *AgentData) Metric(agentID, name string) (MetricView, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	a := d.agents[agentID]
	if a == nil {
		return MetricView{}, false
	}
	e := a.containers[name]
	if e == nil || e.metric == nil {
		return MetricView{}, false
	}
	stale := !a.connected || e.c.State != "running" || d.now().Sub(e.metricAt) > MetricsStale
	return MetricView{Metric: *e.metric, At: e.metricAt, Stale: stale}, true
}

// Logs returns the last log lines of a container, oldest first.
func (d *AgentData) Logs(agentID, name string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	a := d.agents[agentID]
	if a == nil || a.containers[name] == nil {
		return nil
	}
	return slices.Clone(a.containers[name].logs)
}

// Connected reports whether the agent's data is live.
func (d *AgentData) Connected(agentID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	a := d.agents[agentID]
	return a != nil && a.connected
}
