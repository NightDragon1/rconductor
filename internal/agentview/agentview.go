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
// (SEC-006).
type Logs struct {
	ServerID  string   `json:"server_id"`
	Container string   `json:"container"`
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated,omitempty"`
}

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

	mu      sync.Mutex
	follows map[string]*follow // by server ID
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
		return Logs{ServerID: serverID, Container: container, Lines: v.data.Logs(agentID, container)}, true
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
		}
	}
}

func (v *View) lines(agentID string, ll agentproto.LogLines) {
	for _, id := range v.servers(agentID, ll.Container) {
		v.bus.PublishAgentData(id, "logs", Logs{ServerID: id, Container: ll.Container, Lines: ll.Lines, Truncated: ll.Truncated})
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
