// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package agentview

// Every way container log lines leave the panel through the agent view
// runs through this file, which belongs to the core area redact (UI-022).

import (
	"context"
	"log/slog"
	"time"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/logging"
)

// Logs is the data of server.<id>.logs; the lines are foreign data
// (SEC-006). Seq numbers the delivery; the hub drops live deliveries that
// a new subscriber's current state already holds (Design 2.3).
type Logs struct {
	ServerID  string   `json:"server_id"`
	Container string   `json:"container"`
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated,omitempty"`
	// Current marks the last lines sent to a new subscriber, which the
	// client may hold already (UI-009).
	Current bool `json:"current,omitempty"`
	// First numbers the first line; Run names the count, which starts
	// over with the panel. With them the client tells new lines from
	// known ones without comparing text (#157).
	First uint64 `json:"first"`
	Run   string `json:"run"`
	Seq   uint64 `json:"-"`
}

// Sequence implements events.Sequenced.
func (l Logs) Sequence() uint64 { return l.Seq }

// Masking masks the log lines of a server (UI-022, D-088).
type Masking interface {
	ServerLines(ctx context.Context, serverID string, lines []string) []string
}

// maxMaskQueue bounds the deliveries of log lines of one agent that wait
// for the masking; one is at most agentproto.MaxLogBytes.
const maxMaskQueue = 64

// dropWarnEvery spaces the warnings about dropped deliveries of an agent.
const dropWarnEvery = time.Minute

// maskIdle ends the worker and frees the queue of an agent without
// deliveries for this long; the next delivery starts them again.
const maskIdle = time.Minute

type delivery struct {
	agentID    string
	ll         agentproto.LogLines
	seq, first uint64
}

// agentQueue holds the deliveries of one agent for the masking, so a flood
// of one agent never drops the lines of another.
type agentQueue struct {
	ch                    chan delivery
	dropped, droppedLines int // since the last warning
	total                 uint64
	warned                time.Time
}

// UseMasking masks every delivery and every replay of log lines (UI-022).
// Live lines are masked apart from the read loop of the agent, in order,
// one queue per agent, so a flood of lines holds neither the answers of
// the agent nor the lines of other agents; a delivery that finds its
// queue full is dropped, never sent unmasked, and the client sees the gap
// in the numbers. Set before the agents connect.
func (v *View) UseMasking(ctx context.Context, m Masking) {
	v.mask, v.maskCtx, v.queues = m, ctx, map[string]*agentQueue{}
	if v.now == nil {
		v.now = time.Now
	}
}

// maskLoop masks the deliveries of one agent in order; after maskIdle
// without any, and when the masking ends, it frees the queue and tells the
// drops not yet warned about.
func (v *View) maskLoop(agentID string, q *agentQueue) {
	idle := time.NewTimer(maskIdle)
	defer idle.Stop()
	for {
		select {
		case <-v.maskCtx.Done():
			v.freeQueue(agentID, q)
			return
		case d := <-q.ch:
			if v.maskCtx.Err() != nil {
				// Ended meanwhile: this one and the waiting ones are dropped.
				v.qmu.Lock()
				q.dropped, q.droppedLines = q.dropped+1, q.droppedLines+len(d.ll.Lines)
				v.qmu.Unlock()
				v.freeQueue(agentID, q)
				return
			}
			v.publishLines(v.maskCtx, d)
			idle.Reset(maskIdle)
		case <-idle.C:
			if v.freeQueue(agentID, q) {
				return
			}
			idle.Reset(maskIdle)
		}
	}
}

// freeQueue removes the queue of an agent unless a delivery waits in it;
// deliveries are queued under qmu, so none can come after. With the
// masking ended the queue goes in any case: its waiting deliveries are
// dropped and counted. It tells the drops not yet warned about.
func (v *View) freeQueue(agentID string, q *agentQueue) bool {
	v.qmu.Lock()
	if v.maskCtx.Err() == nil && len(q.ch) > 0 {
		v.qmu.Unlock()
		return false
	}
	for len(q.ch) > 0 {
		d := <-q.ch
		q.dropped, q.droppedLines = q.dropped+1, q.droppedLines+len(d.ll.Lines)
	}
	if v.queues[agentID] == q {
		delete(v.queues, agentID)
	}
	n, lines := q.dropped, q.droppedLines
	v.qmu.Unlock()
	if n > 0 {
		v.dropWarn(agentID, n, lines)
	}
	return true
}

func (v *View) dropWarn(agentID string, deliveries, lines int) {
	v.log.Warn("log lines dropped: masking is behind", logging.Event(logging.LogsLinesDropped), slog.String("agent_id", agentID),
		slog.Int("deliveries", deliveries), slog.Int("lines", lines))
}

func (v *View) masked(ctx context.Context, serverID string, lines []string) []string {
	if v.mask == nil {
		// Not wired: no line leaves unmasked (fail closed).
		out := make([]string, len(lines))
		for i := range out {
			out[i] = withheld
		}
		return out
	}
	return v.mask.ServerLines(ctx, serverID, lines)
}

// withheld stands for a line while no masking is set up.
const withheld = "••• (line withheld: masking not set up)"

// currentLogs is the replay for a new subscriber, masked as live lines are.
func (v *View) currentLogs(ctx context.Context, serverID, agentID, container string) Logs {
	lines, seq, first := v.data.Logs(agentID, container)
	return Logs{ServerID: serverID, Container: container, Lines: v.masked(ctx, serverID, lines), Current: true, First: first, Run: v.run, Seq: seq}
}

func (v *View) lines(agentID string, ll agentproto.LogLines, seq, first uint64) {
	d := delivery{agentID: agentID, ll: ll, seq: seq, first: first}
	if v.queues == nil {
		v.publishLines(context.Background(), d)
		return
	}
	if v.maskCtx.Err() != nil {
		return // the panel stops: no new worker, the line is not counted
	}
	v.qmu.Lock()
	q := v.queues[agentID]
	if q == nil {
		q = &agentQueue{ch: make(chan delivery, maxMaskQueue)}
		v.queues[agentID] = q // one per agent with deliveries, freed when idle
		go v.maskLoop(agentID, q)
	}
	select {
	case q.ch <- d:
		v.qmu.Unlock()
		return
	default:
	}
	q.dropped, q.droppedLines, q.total = q.dropped+1, q.droppedLines+len(ll.Lines), q.total+1
	now := v.now()
	warn := q.warned.IsZero() || now.Sub(q.warned) >= dropWarnEvery
	n, lines := q.dropped, q.droppedLines
	if warn {
		q.dropped, q.droppedLines, q.warned = 0, 0, now
	}
	v.qmu.Unlock()
	if warn {
		v.dropWarn(agentID, n, lines)
	}
}

// droppedDeliveries is the number of deliveries of an agent dropped since
// its queue was started.
func (v *View) droppedDeliveries(agentID string) uint64 {
	v.qmu.Lock()
	defer v.qmu.Unlock()
	if q := v.queues[agentID]; q != nil {
		return q.total
	}
	return 0
}

func (v *View) publishLines(ctx context.Context, d delivery) {
	for _, id := range v.servers(d.agentID, d.ll.Container) {
		v.bus.PublishAgentData(id, "logs", Logs{ServerID: id, Container: d.ll.Container, Lines: v.masked(ctx, id, d.ll.Lines),
			Truncated: d.ll.Truncated, First: d.first, Run: v.run, Seq: d.seq})
	}
}
