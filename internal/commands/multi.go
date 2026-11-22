// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/commands/limits"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/users"
)

// ResultNotFound is the result of one server of a multi-server command
// besides those of the history: the server is outside the scope or
// unknown.
const ResultNotFound = "not_found"

// ResultSkipped and ReasonTimeBudget: a server of a multi-server command
// whose turn came after the time budget; it is not sent the command and
// its audit entry is an error with this reason (CMD-010, D-069).
const (
	ResultSkipped    = "skipped"
	ReasonTimeBudget = "time_budget"
)

// multiAnswerBy is when the answer of a multi-server command leaves at the
// latest, before the HTTP write deadline of 60 s; further runs start only
// while one still ends in time (a run takes at most the command timeout).
// The first run that passes the checks (known, in scope, list, rate
// limit) always goes out, also when it alone fills the budget.
const multiAnswerBy = 55 * time.Second

// MultiItem is the result of one server of a multi-server command.
type MultiItem struct {
	ServerID string
	// RequestID is empty for not_found.
	RequestID string
	Result    string
	Reason    string
	Detail    string
	Command   string
	Response  string
	Truncated bool
}

func (s *Service) multiSetting(ctx context.Context, key string, def, lo, hi int) int {
	n := def
	if err := s.st.GuardedSetting(ctx, s.log, key, &n); err != nil || n < lo || n > hi {
		n = def
	}
	return n
}

// Multi sends one command to several servers (CMD-010, D-069): only RCON
// admin and server admin; every server gets a normal run of Send with its
// own request ID, its own scope check and the rate limit of its bucket
// (user, server), as a single command would. The runs share a batch ID
// the panel makes.
func (s *Service) Multi(ctx context.Context, req users.Request, serverIDs []string, input string) (string, []MultiItem, error) {
	// The role comes before anything else (revision 2.4 candidate 14).
	if req.Actor.Role != perm.RCONAdmin && req.Actor.Role != perm.ServerAdmin {
		perm.LogDenied(s.log, perm.Denial{
			RequestID: req.RequestID, SessionRef: req.SessionRef, User: req.Pseudonym,
			Role: req.Actor.Role, Reason: "forbidden", Action: "command.multi",
		})
		if _, err := audit.Write(ctx, s.st.DB, audit.Entry{
			Kind: audit.KindCommand, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
			Action: "command.multi", Result: audit.ResultDenied, Reason: "forbidden",
		}); err != nil {
			s.log.Error("audit entry not written", "action", "command.multi", "err", err)
		}
		return "", nil, users.ErrForbidden
	}
	ids := make([]string, 0, len(serverIDs))
	seen := map[string]bool{}
	for _, id := range serverIDs {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return "", nil, &users.InvalidError{Detail: "name at least one server"}
	}
	if strings.TrimSpace(input) == "" {
		return "", nil, &users.InvalidError{Detail: "a command is required"}
	}
	limit := s.multiSetting(ctx, users.SettingMultiMaxServers, limits.DefaultMultiMaxServers, limits.MinMultiMaxServers, limits.MaxMultiMaxServers)
	if len(ids) > limit {
		return "", nil, &users.InvalidError{Detail: fmt.Sprintf("at most %d servers at once", limit)}
	}
	batch, err := store.NewID()
	if err != nil {
		return "", nil, err
	}
	parallel := s.multiSetting(ctx, users.SettingMultiParallel, limits.DefaultMultiParallel, limits.MinMultiParallel, limits.MaxMultiParallel)
	items := make([]MultiItem, len(ids))
	slots := make(chan struct{}, parallel)
	// Until a run has passed its checks, runs start one after the other
	// without the deadline; then further runs start until the deadline,
	// so a run started then still ends in time.
	first := true
	deadline := s.now().Add(max(multiAnswerBy-s.timeout, 0))
	over := false
	var wg sync.WaitGroup
	for i, id := range ids {
		r := req
		r.BatchID = batch
		if r.RequestID, err = store.NewID(); err != nil {
			return "", nil, err
		}
		if first && ctx.Err() != nil {
			// The client left before a run went out: the rest is skipped
			// and audited all the same.
			first, over = false, true
		}
		if first {
			slots <- struct{}{}
			out, done := make(chan struct{}), make(chan struct{})
			rctx := context.WithValue(ctx, passedKey{}, func() { close(out) })
			wg.Go(func() {
				defer func() { <-slots }()
				defer close(done)
				items[i] = s.multiOne(rctx, r, id, input)
			})
			select {
			case <-out:
				first = false
			case <-done:
				select {
				case <-out: // sent and already done
					first = false
				default: // refused or failed before sending: the next one is first
				}
			}
			continue
		}
		if !over {
			wait := time.NewTimer(deadline.Sub(s.now()))
			select {
			case slots <- struct{}{}:
				if !s.now().Before(deadline) {
					<-slots // the slot came too late
					over = true
				}
			case <-wait.C:
				over = true
			}
			wait.Stop()
		}
		if over {
			items[i] = s.multiSkipped(ctx, r, id, input)
			continue
		}
		wg.Go(func() {
			defer func() { <-slots }()
			items[i] = s.multiOne(ctx, r, id, input)
		})
	}
	wg.Wait()
	return batch, items, nil
}

// multiSkipped is a server whose turn came after the time budget: in the
// scope it gets its own audit entry, an error with the reason time_budget,
// under its request ID and the batch; outside it is not_found as in Send.
func (s *Service) multiSkipped(ctx context.Context, req users.Request, serverID, input string) MultiItem {
	rights, err := s.perm.For(ctx, req.Actor.ID)
	if err != nil {
		s.log.Error("multi command failed", append(s.attrs(req, serverID), "err", err)...)
		return MultiItem{ServerID: serverID, RequestID: req.RequestID, Result: ResultError}
	}
	if _, ok := rights.Server(serverID); !ok {
		perm.LogDenied(s.log, perm.Denial{
			RequestID: req.RequestID, SessionRef: req.SessionRef, User: req.Pseudonym, Role: req.Actor.Role,
			Server: serverID, Reason: "not_found", Action: "command",
		})
		return MultiItem{ServerID: serverID, Result: ResultNotFound, Reason: "not_found"}
	}
	s.log.Warn("multi command: server left out after the time budget",
		append(s.attrs(req, serverID), logging.Event(logging.CmdDenied), slog.String(logging.KeyReason, ReasonTimeBudget))...)
	// Unsent, so uncleaned: kept short as a refused input is. The entry is
	// written even when the client has gone (one per server, CMD-010).
	command := truncate(input, 256)
	if err := s.entry(context.WithoutCancel(ctx), req, serverID, ActionDenied, audit.Entry{
		Payload: payload{Command: command, BatchID: req.BatchID}, Result: audit.ResultError, Reason: ReasonTimeBudget,
	}); err != nil {
		s.log.Error("audit entry not written", append(s.attrs(req, serverID), "err", err)...)
	}
	s.publish(serverID, Entry{
		RequestID: req.RequestID, TS: s.now().UTC(), UserID: req.Actor.ID, Pseudonym: req.Pseudonym,
		Role: string(req.Actor.Role), Command: command, Result: ResultError, Reason: ReasonTimeBudget,
	})
	return MultiItem{ServerID: serverID, RequestID: req.RequestID, Result: ResultSkipped, Reason: ReasonTimeBudget}
}

func (s *Service) multiOne(ctx context.Context, req users.Request, serverID, input string) MultiItem {
	res, err := s.Send(ctx, req, serverID, input)
	it := MultiItem{ServerID: serverID, RequestID: req.RequestID, Result: ResultOK, Command: res.Command,
		Response: res.Response, Truncated: res.Truncated}
	if err == nil && res.Result == ResultError {
		it.Result, it.Reason = ResultError, res.Reason
	}
	var denied *DeniedError
	switch {
	case err == nil:
	case errors.Is(err, users.ErrNotFound):
		return MultiItem{ServerID: serverID, Result: ResultNotFound, Reason: "not_found"}
	case errors.As(err, &denied):
		it.Result, it.Reason, it.Detail = ResultDenied, denied.Reason, denied.Detail
	case errors.Is(err, ErrTimeout):
		it.Result, it.Reason = ResultTimeout, ReasonTimeout
	case errors.Is(err, ErrBusy):
		it.Result, it.Reason = ResultError, "busy"
	case errors.Is(err, ErrUnavailable):
		it.Result, it.Reason = ResultError, "server_unavailable"
	default:
		s.log.Error("multi command failed", append(s.attrs(req, serverID), "err", err)...)
		it.Result = ResultError
	}
	return it
}

// passedKey marks the context of a run that reports when it passed every
// check of Send, right before the command goes out.
type passedKey struct{}

// passed reports a run of Send that passed its checks to Multi, if it waits
// for that.
func passed(ctx context.Context) {
	if f, ok := ctx.Value(passedKey{}).(func()); ok {
		f()
	}
}
