// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"log/slog"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/users"
)

// ResultNotFound is the result of one server of a multi-server command
// besides those of the history: the server is outside the scope or
// unknown.
const ResultNotFound = "not_found"

// MultiBudget bounds how long a multi-server command starts new runs: a
// run takes at most Timeout, so the answer leaves before the HTTP write
// deadline of 60 s (CMD-010). Servers whose turn comes later get the
// result timeout without being sent the command.
const MultiBudget = 45 * time.Second

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

func (s *Service) multiSetting(ctx context.Context, key string, def, lo int) int {
	n := def
	if err := s.st.GuardedSetting(ctx, s.log, key, &n); err != nil || n < lo {
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
	limit := s.multiSetting(ctx, users.SettingMultiMaxServers, users.DefaultMultiMaxServers, users.MinMultiMaxServers)
	if len(ids) > limit {
		return "", nil, &users.InvalidError{Detail: fmt.Sprintf("at most %d servers at once", limit)}
	}
	batch, err := store.NewID()
	if err != nil {
		return "", nil, err
	}
	parallel := s.multiSetting(ctx, users.SettingMultiParallel, users.DefaultMultiParallel, users.MinMultiParallel)
	items := make([]MultiItem, len(ids))
	slots := make(chan struct{}, parallel)
	budget := time.NewTimer(s.multiBudget)
	defer budget.Stop()
	late := 0
	var wg sync.WaitGroup
	for i, id := range ids {
		r := req
		r.BatchID = batch
		if r.RequestID, err = store.NewID(); err != nil {
			return "", nil, err
		}
		select {
		case slots <- struct{}{}:
		case <-budget.C:
			// From here on no run starts; the timer fired once, so the
			// rest is late at once.
			for k := i; k < len(ids); k++ {
				items[k] = MultiItem{ServerID: ids[k], Result: ResultTimeout, Reason: ReasonTimeout}
			}
			late = len(ids) - i
		}
		if late > 0 {
			break
		}
		wg.Go(func() {
			defer func() { <-slots }()
			items[i] = s.multiOne(ctx, r, id, input)
		})
	}
	wg.Wait()
	if late > 0 {
		s.log.Warn("multi command: servers left out after the time budget",
			append(s.attrs(req, ""), slog.String("batch_id", batch), slog.Int("servers", late))...)
	}
	return batch, items, nil
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
