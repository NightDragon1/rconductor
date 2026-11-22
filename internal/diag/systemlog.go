// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package diag

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/agenthub"
	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
	"github.com/NightDragon1/rconductor/internal/syslog"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Answers about the log of an agent (UI-025).
var (
	ErrAgentUnavailable = errors.New("the agent is not connected")
	ErrAgentUnsupported = errors.New("the agent does not send its own log")
	ErrAgentFailed      = errors.New("the agent could not read its log")
	// ErrAgentBusy: the agent refused for its rate limit; later again (503).
	ErrAgentBusy = errors.New("the agent takes no more orders for now")
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
// for the panel, the period (400). The first read of a source in a
// session is audited without content, whatever After says; polling after
// it is not.
func (s *Service) SystemLog(ctx context.Context, req users.Request, q LogQuery) (Page, error) {
	if req.Actor.Role != perm.RCONAdmin {
		return Page{}, s.denied(ctx, req, "system_log.viewed")
	}
	_, known := ranks[q.Level]
	switch {
	case q.Limit < 1 || q.Limit > maxLogLimit:
		return Page{}, &users.InvalidError{Detail: "limit is 1 to 500"}
	case !known:
		return Page{}, &users.InvalidError{Detail: "level is info, warn or error"}
	case q.After < 0:
		return Page{}, &users.InvalidError{Detail: "after is at least 1"}
	}
	if q.Source != "panel" {
		if _, err := uuid.Parse(q.Source); err != nil {
			return Page{}, &users.InvalidError{Detail: "source is panel or the ID of an agent"}
		}
	} else if err := period(q.From, q.To); err != nil {
		return Page{}, err
	}
	if s.firstRead(req.SessionID, q.Source, q.After) {
		if _, err := audit.Write(ctx, s.st.DB, audit.Entry{
			Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
			TargetType: "diagnostics", TargetID: q.Source, Action: "system_log.viewed", Result: audit.ResultOK,
		}); err != nil {
			return Page{}, err
		}
		s.markRead(req.SessionID, q.Source)
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

// firstRead tells whether a read is audited: every call without After,
// and the first call of a session for a source.
func (s *Service) firstRead(session, source string, after int64) bool {
	if after == 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.viewed[session+"\x00"+source]
}

// markRead notes the audited read of a source in a session.
func (s *Service) markRead(session, source string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.viewed == nil {
		s.viewed = map[string]bool{}
	}
	s.viewed[session+"\x00"+source] = true
}

// agentLog reads the last lines of the agent's own container over
// logs.tail (D-091), masks them as container logs and keeps those from
// the level up; a line without a level stays.
func (s *Service) agentLog(ctx context.Context, q LogQuery) (Page, error) {
	if s.mask == nil {
		return Page{}, ErrAgentUnavailable
	}
	lines, cut, err := s.tail(ctx, q.Source, q.Limit)
	if err != nil {
		return Page{}, err
	}
	p := Page{Source: q.Source, Truncated: cut, Items: []Entry{}}
	held := 0
	for _, l := range lines {
		// The level from the line as it came: masking may change its keys.
		var head struct {
			Level string `json:"level"`
		}
		known := json.Unmarshal([]byte(l), &head) == nil
		// The level is foreign data: only a known one is kept, in the
		// spelling of the list (slog writes INFO, WARN, ERROR); any other
		// is none (SEC-006).
		level := strings.ToLower(head.Level)
		if _, ok := ranks[level]; !ok {
			level = ""
		}
		// The agent logs JSON: each value is masked as it was logged.
		var n int
		l, n = syslog.MaskLineCounted(l, s.mask)
		held += n
		if known && level != "" && ranks[level] < ranks[q.Level] {
			continue
		}
		p.Items = append(p.Items, Entry{Level: level, Text: l})
	}
	if held > 0 {
		// Values held back for the time budget: the page is not whole.
		p.Truncated = true
		s.log.Warn("agent log values withheld: masking took too long", slog.String("source", q.Source), slog.Int("values", held))
	}
	return p, nil
}

// tail fetches the last lines of the agent's own container over logs.tail
// (D-091), unmasked and bounded to agentproto.MaxLogLines.
func (s *Service) tail(ctx context.Context, agentID string, n int) ([]string, bool, error) {
	if s.agents == nil {
		return nil, false, ErrAgentUnavailable
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
	res, err := s.agents.RequestData(tctx, agentID, agentproto.TypeLogsTail,
		agentproto.LogsTail{Container: agentproto.SelfContainer, Lines: n}, collect)
	switch {
	case errors.Is(err, agenthub.ErrNotCapable):
		return nil, false, ErrAgentUnsupported
	case errors.Is(err, agenthub.ErrNotConnected), errors.Is(err, agenthub.ErrBusy), errors.Is(err, context.DeadlineExceeded):
		return nil, false, ErrAgentUnavailable
	case err != nil:
		return nil, false, err
	case res.Code == agentproto.CodeRateLimited:
		return nil, false, ErrAgentBusy
	case res.Code == agentproto.CodeForbidden:
		return nil, false, ErrAgentUnsupported // an agent before 1.2 or outside a container
	case res.Code != agentproto.CodeOK:
		return nil, false, ErrAgentFailed
	}
	lines, cut := agentproto.LimitLines(got.Lines)
	return lines, got.Truncated || cut, nil
}

// maxLogLimit bounds a page of the system log as the contract does.
const maxLogLimit = 500
