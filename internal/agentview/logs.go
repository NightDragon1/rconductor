// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package agentview

// Every way container log lines leave the panel through the agent view
// runs through this file, which belongs to the core area redact (UI-022).

import (
	"context"
	"log/slog"

	"github.com/NightDragon1/rconductor/internal/agentproto"
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

// maxMaskQueue bounds the deliveries of log lines that wait for the
// masking; one is at most agentproto.MaxLogBytes.
const maxMaskQueue = 64

type delivery struct {
	agentID    string
	ll         agentproto.LogLines
	seq, first uint64
}

// UseMasking masks every delivery and every replay of log lines (UI-022).
// Live lines are masked apart from the read loop of the agent, so a flood
// of lines does not hold its answers; a delivery that finds the queue full
// is dropped, never sent unmasked, and the client sees the gap in the
// numbers. Set before the agents connect.
func (v *View) UseMasking(ctx context.Context, m Masking) {
	v.mask, v.queue = m, make(chan delivery, maxMaskQueue)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case d := <-v.queue:
				v.publishLines(ctx, d)
			}
		}
	}()
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
	if v.queue == nil {
		v.publishLines(context.Background(), d)
		return
	}
	select {
	case v.queue <- d:
	default:
		v.log.Warn("log lines dropped: masking is behind", slog.String("agent_id", agentID),
			slog.String("container", ll.Container), slog.Int("lines", len(ll.Lines)))
	}
}

func (v *View) publishLines(ctx context.Context, d delivery) {
	for _, id := range v.servers(d.agentID, d.ll.Container) {
		v.bus.PublishAgentData(id, "logs", Logs{ServerID: id, Container: d.ll.Container, Lines: v.masked(ctx, id, d.ll.Lines),
			Truncated: d.ll.Truncated, First: d.first, Run: v.run, Seq: d.seq})
	}
}
