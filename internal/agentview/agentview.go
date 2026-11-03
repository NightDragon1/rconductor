// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agentview connects the agent data in the cache with the servers
// and the event bus: it maps containers to servers, serves the topics
// server.<id>.metrics and server.<id>.logs, and follows logs only while
// someone views them (UI-009, AGT-003).
package agentview

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/cache"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// Agents sends orders to agents.
type Agents interface {
	Request(ctx context.Context, agentID, typ string, payload any) (agentproto.Result, error)
}

// Bus publishes to the WebSocket topics.
type Bus interface {
	PublishAgentData(serverID, kind string, data any)
	PublishRuntime(serverID string, full, reduced any)
}

// ContainerRuntime is the agent part of a server's runtime data; only
// RCON admin and server admin see it (UI-006).
type ContainerRuntime struct {
	Name           string     `json:"name"`
	Image          string     `json:"image,omitempty"`
	State          string     `json:"state"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	RestartCount   int        `json:"restart_count"`
	RestartLoop    bool       `json:"restart_loop"`
	CPUPercent     *float64   `json:"cpu_percent,omitempty"`
	MemBytes       *uint64    `json:"mem_bytes,omitempty"`
	MemLimit       *uint64    `json:"mem_limit,omitempty"`
	MetricsAt      *time.Time `json:"metrics_at,omitempty"`
	Stale          bool       `json:"stale"`
	AgentConnected bool       `json:"agent_connected"`
}

// Runtime is what cards and the server head show (revision 2.4 candidates
// 3 and 4); kind is runtime in servers.overview.
type Runtime struct {
	Kind         string            `json:"kind"`
	ServerID     string            `json:"server_id"`
	Players      *int              `json:"players,omitempty"`
	PlayersAt    *time.Time        `json:"players_at,omitempty"`
	PlayersStale bool              `json:"players_stale,omitempty"`
	Container    *ContainerRuntime `json:"container,omitempty"`
}

// Metrics is the data of server.<id>.metrics.
type Metrics struct {
	ServerID   string    `json:"server_id"`
	Container  string    `json:"container"`
	State      string    `json:"state"`
	CPUPercent float64   `json:"cpu_percent"`
	MemBytes   uint64    `json:"mem_bytes"`
	MemLimit   uint64    `json:"mem_limit"`
	NetRx      uint64    `json:"net_rx"`
	NetTx      uint64    `json:"net_tx"`
	BlkRead    uint64    `json:"blk_read"`
	BlkWrite   uint64    `json:"blk_write"`
	At         time.Time `json:"at"`
	Stale      bool      `json:"stale"`
}

// Logs is the data of server.<id>.logs; the lines are foreign data
// (SEC-006). Seq numbers the delivery; the hub drops live deliveries that
// a new subscriber's current state already holds (Design 2.3).
type Logs struct {
	ServerID  string   `json:"server_id"`
	Container string   `json:"container"`
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated,omitempty"`
	Seq       uint64   `json:"-"`
}

// Sequence implements events.Sequenced.
func (l Logs) Sequence() uint64 { return l.Seq }

type follow struct {
	agentID, container string
	viewers            int
}

// View serves the agent topics.
type View struct {
	st     *store.Store
	data   *cache.AgentData
	agents Agents
	bus    Bus
	log    *slog.Logger

	players *cache.Cache

	mu      sync.Mutex
	follows map[string]*follow // by server ID
}

// SetPlayers adds the player counts to the runtime data.
func (v *View) SetPlayers(p *cache.Cache) {
	v.players = p
	p.OnPlayers(func(id string) { v.publishRuntime(context.Background(), id) })
}

// Runtime returns the runtime data of a server; agentData adds the
// container part.
func (v *View) Runtime(ctx context.Context, serverID string, agentData bool) Runtime {
	rt := Runtime{Kind: "runtime", ServerID: serverID}
	if v.players != nil {
		if p, ok := v.players.Cached(serverID); ok {
			n, at := len(p.Names), p.UpdatedAt
			rt.Players, rt.PlayersAt, rt.PlayersStale = &n, &at, p.Stale
		}
	}
	if !agentData {
		return rt
	}
	agentID, name, ok := v.container(ctx, serverID)
	if !ok {
		return rt
	}
	cr := &ContainerRuntime{Name: name, State: cache.StateUnknown, AgentConnected: v.data.Connected(agentID), Stale: true}
	if c, ok := v.data.Container(agentID, name); ok {
		cr.State, cr.Image, cr.RestartCount, cr.RestartLoop = c.State, c.Image, c.RestartCount, c.RestartLoop
		if !c.StartedAt.IsZero() {
			t := c.StartedAt
			cr.StartedAt = &t
		}
	}
	if m, ok := v.data.Metric(agentID, name); ok {
		cpu, mem, limit, at := m.CPUPercent, m.MemBytes, m.MemLimit, m.At
		cr.CPUPercent, cr.MemBytes, cr.MemLimit, cr.MetricsAt, cr.Stale = &cpu, &mem, &limit, &at, m.Stale
	}
	rt.Container = cr
	return rt
}

// Container is a container an agent offers, for the server form.
type Container struct {
	Name  string
	Image string
	State string
}

// Containers lists the containers of an agent and whether it is
// connected (revision 2.4 candidate 22).
func (v *View) Containers(agentID string) ([]Container, bool) {
	var out []Container
	for _, c := range v.data.Containers(agentID) {
		out = append(out, Container{Name: c.Name, Image: c.Image, State: c.State})
	}
	return out, v.data.Connected(agentID)
}

func (v *View) publishRuntime(ctx context.Context, serverID string) {
	v.bus.PublishRuntime(serverID, v.Runtime(ctx, serverID, true), v.Runtime(ctx, serverID, false))
}

// New wires the cache's notifications to the bus.
func New(st *store.Store, data *cache.AgentData, agents Agents, bus Bus, log *slog.Logger) *View {
	v := &View{st: st, data: data, agents: agents, bus: bus, log: log, follows: map[string]*follow{}}
	data.OnMetrics(v.metrics)
	data.OnChange(func(agentID, container string) { v.metrics(agentID, []string{container}) })
	data.OnLines(v.lines)
	data.OnConnect(v.reconnected)
	return v
}

// container returns the agent and container of a server.
func (v *View) container(ctx context.Context, serverID string) (agentID, name string, ok bool) {
	r, err := v.st.Queries.GetServer(ctx, serverID)
	if err != nil || !r.AgentID.Valid || !r.ContainerName.Valid {
		return "", "", false
	}
	return r.AgentID.String, r.ContainerName.String, true
}

func (v *View) servers(agentID, container string) []string {
	ids, err := v.st.Queries.ListServersByContainer(context.Background(), sqlitedb.ListServersByContainerParams{
		AgentID: sql.NullString{String: agentID, Valid: true}, ContainerName: sql.NullString{String: container, Valid: true},
	})
	if err != nil {
		v.log.Error("servers of a container not found", slog.Any("err", err))
		return nil
	}
	return ids
}

func (v *View) metricsOf(serverID, agentID, container string) (Metrics, bool) {
	m, ok := v.data.Metric(agentID, container)
	if !ok {
		return Metrics{}, false
	}
	out := Metrics{
		ServerID: serverID, Container: container, CPUPercent: m.CPUPercent, MemBytes: m.MemBytes, MemLimit: m.MemLimit,
		NetRx: m.NetRx, NetTx: m.NetTx, BlkRead: m.BlkRead, BlkWrite: m.BlkWrite, At: m.At, Stale: m.Stale,
	}
	if c, ok := v.data.Container(agentID, container); ok {
		out.State = c.State
	}
	return out, true
}

// Current implements events.AgentViews.
func (v *View) Current(ctx context.Context, serverID, kind string) (any, bool) {
	agentID, container, ok := v.container(ctx, serverID)
	if !ok {
		return nil, false
	}
	switch kind {
	case "metrics":
		return v.metricsOf(serverID, agentID, container)
	case "logs":
		lines, seq := v.data.Logs(agentID, container)
		return Logs{ServerID: serverID, Container: container, Lines: lines, Seq: seq}, true
	}
	return nil, false
}

// metrics publishes the current sample of the containers, also after a
// change of state or a lost agent, so that stale values show at once
// (NFR-005).
func (v *View) metrics(agentID string, containers []string) {
	for _, c := range containers {
		for _, id := range v.servers(agentID, c) {
			if m, ok := v.metricsOf(id, agentID, c); ok {
				v.bus.PublishAgentData(id, "metrics", m)
			}
			v.publishRuntime(context.Background(), id)
		}
	}
}

func (v *View) lines(agentID string, ll agentproto.LogLines, seq uint64) {
	for _, id := range v.servers(agentID, ll.Container) {
		v.bus.PublishAgentData(id, "logs", Logs{ServerID: id, Container: ll.Container, Lines: ll.Lines, Truncated: ll.Truncated, Seq: seq})
	}
}

// FollowLogs implements events.AgentViews: the first viewer of a server
// starts logs.follow, the last one sends logs.stop (UI-009).
func (v *View) FollowLogs(ctx context.Context, serverID string) func() {
	agentID, container, ok := v.container(ctx, serverID)
	if !ok {
		return func() {}
	}
	v.mu.Lock()
	f := v.follows[serverID]
	if f == nil {
		f = &follow{agentID: agentID, container: container}
		v.follows[serverID] = f
	}
	f.viewers++
	first := f.viewers == 1
	v.mu.Unlock()
	if first {
		go v.order(ctx, agentID, agentproto.TypeLogsFollow, container)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			v.mu.Lock()
			f.viewers--
			last := f.viewers == 0
			if last {
				delete(v.follows, serverID)
			}
			v.mu.Unlock()
			if last {
				go v.order(ctx, agentID, agentproto.TypeLogsStop, container)
			}
		})
	}
}

// reconnected starts the follows of an agent again: its earlier session
// took them along (Design 1.5).
func (v *View) reconnected(agentID string) {
	v.mu.Lock()
	var containers []string
	for _, f := range v.follows {
		if f.agentID == agentID {
			containers = append(containers, f.container)
		}
	}
	v.mu.Unlock()
	for _, c := range containers {
		go v.order(context.Background(), agentID, agentproto.TypeLogsFollow, c)
	}
}

// order sends a log order; it outlives the subscription that caused it.
func (v *View) order(ctx context.Context, agentID, typ, container string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	r, err := v.agents.Request(ctx, agentID, typ, agentproto.LogsFollow{Container: container})
	if err != nil || r.Code != agentproto.CodeOK {
		v.log.Info("log order not carried out", slog.String("agent_id", agentID), slog.String("type", typ),
			slog.String("container", container), slog.String("code", r.Code), slog.Any("err", err))
		return
	}
	code := logging.LogsFollowStart
	if typ == agentproto.TypeLogsStop {
		code = logging.LogsFollowStop
	}
	v.log.Info("container logs", logging.Event(code), slog.String("agent_id", agentID), slog.String("container", container))
}
