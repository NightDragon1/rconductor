// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package diag

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/agenthub"
	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Answers about the log of an agent (UI-025).
var (
	ErrAgentUnavailable = errors.New("the agent is not connected")
	ErrAgentUnsupported = errors.New("the agent does not send its own log")
	ErrAgentFailed      = errors.New("the agent could not read its log")
)

const agentTimeout = 15 * time.Second

// Agents is what the log of an agent needs from the agent hub.
type Agents interface {
	RequestData(ctx context.Context, agentID, typ string, payload any, collect func(agentproto.Envelope)) (agentproto.Result, error)
}

// Masker masks lines as for container logs (logmask.Provider, D-088).
type Masker interface {
	Lines(lines []string, profilePatterns [][]string) ([]string, int)
}

// Use links the agent hub and the masking for the log of an agent.
func (s *Service) Use(a Agents, m Masker) { s.agents, s.mask = a, m }

// LogQuery selects entries of the system log (UI-025, D-091): Source is
// "panel" or the ID of an agent; From, To and After apply to the panel.
type LogQuery struct {
	Source   string
	From, To time.Time
	Level    string // info, warn or error: the lowest level shown
	After    int64
	Limit    int
}

// Entry is one entry; ID and Time only for the panel.
type Entry struct {
	ID                int64
	Time              time.Time
	Level, Code, Text string
}

// Page is the answer of SystemLog.
type Page struct {
	Source    string
	Items     []Entry
	Truncated bool
}

var ranks = map[string]int{"info": 0, "warn": 1, "error": 2}

// SystemLog checks the role first (403, audited), then the source and,
// for the panel, the period (400). A call without After is audited
// without content; polling with After is not.
func (s *Service) SystemLog(ctx context.Context, req users.Request, q LogQuery) (Page, error) {
	if req.Actor.Role != perm.RCONAdmin {
		return Page{}, s.denied(ctx, req, "system_log.viewed")
	}
	if q.Source != "panel" {
		if _, err := uuid.Parse(q.Source); err != nil {
			return Page{}, &users.InvalidError{Detail: "source is panel or the ID of an agent"}
		}
	} else if err := period(q.From, q.To); err != nil {
		return Page{}, err
	}
	if q.After == 0 {
		if _, err := audit.Write(ctx, s.st.DB, audit.Entry{
			Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
			TargetType: "diagnostics", TargetID: q.Source, Action: "system_log.viewed", Result: audit.ResultOK,
		}); err != nil {
			return Page{}, err
		}
	}
	if q.Source != "panel" {
		return s.agentLog(ctx, q)
	}
	rows, err := s.st.Queries.ListSystemLog(ctx, sqlitedb.ListSystemLogParams{
		FromTs: q.From.UTC().Format(store.TimeLayout), ToTs: q.To.UTC().Format(store.TimeLayout),
		After: q.After, MinRank: int64(ranks[q.Level]), MaxRows: int64(q.Limit),
	})
	if err != nil {
		return Page{}, err
	}
	p := Page{Source: q.Source, Items: make([]Entry, 0, len(rows))}
	for _, r := range rows {
		ts, _ := time.Parse(store.TimeLayout, r.Ts)
		p.Items = append(p.Items, Entry{ID: r.ID, Time: ts, Level: r.Level, Code: r.Code, Text: r.Text})
	}
	return p, nil
}

// agentLog reads the last lines of the agent's own container over
// logs.tail (D-091), masks them as container logs and keeps those from
// the level up; a line without a level stays.
func (s *Service) agentLog(ctx context.Context, q LogQuery) (Page, error) {
	if s.agents == nil || s.mask == nil {
		return Page{}, ErrAgentUnavailable
	}
	var got agentproto.LogLines
	collect := func(e agentproto.Envelope) {
		if ll, err := agentproto.Payload[agentproto.LogLines](e); err == nil && e.Type == agentproto.TypeLogLines {
			lines, cut := agentproto.LimitLines(append(got.Lines, ll.Lines...))
			got.Lines, got.Truncated = lines, got.Truncated || ll.Truncated || cut
		}
	}
	tctx, cancel := context.WithTimeout(ctx, agentTimeout)
	defer cancel()
	res, err := s.agents.RequestData(tctx, q.Source, agentproto.TypeLogsTail,
		agentproto.LogsTail{Container: agentproto.SelfContainer, Lines: q.Limit}, collect)
	switch {
	case errors.Is(err, agenthub.ErrNotCapable):
		return Page{}, ErrAgentUnsupported
	case errors.Is(err, agenthub.ErrNotConnected), errors.Is(err, agenthub.ErrBusy), errors.Is(err, context.DeadlineExceeded):
		return Page{}, ErrAgentUnavailable
	case err != nil:
		return Page{}, err
	case res.Code == agentproto.CodeForbidden:
		return Page{}, ErrAgentUnsupported // an agent before 1.2 or outside a container
	case res.Code != agentproto.CodeOK:
		return Page{}, ErrAgentFailed
	}
	lines, cut := agentproto.LimitLines(got.Lines)
	lines, _ = s.mask.Lines(lines, nil)
	p := Page{Source: q.Source, Truncated: got.Truncated || cut, Items: []Entry{}}
	for _, l := range lines {
		var head struct {
			Level string `json:"level"`
		}
		if json.Unmarshal([]byte(l), &head) == nil && ranks[head.Level] < ranks[q.Level] {
			continue
		}
		p.Items = append(p.Items, Entry{Level: head.Level, Text: l})
	}
	return p, nil
}
