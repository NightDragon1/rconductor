// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/users"
)

// ResultNotFound is the result of one server of a multi-server command
// besides those of the history: the server is outside the scope or
// unknown.
const ResultNotFound = "not_found"

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
	if err := s.st.GlobalSetting(ctx, key, &n); err != nil || n < lo {
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
	var wg sync.WaitGroup
	for i, id := range ids {
		r := req
		r.BatchID = batch
		if r.RequestID, err = store.NewID(); err != nil {
			return "", nil, err
		}
		slots <- struct{}{}
		wg.Go(func() {
			defer func() { <-slots }()
			items[i] = s.multiOne(ctx, r, id, input)
		})
	}
	wg.Wait()
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
