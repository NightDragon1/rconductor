// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package diag builds the diagnostic export (NFR-018, D-090): one ZIP for
// the RCON admin, blacked out before writing.
package diag

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/users"
)

// MaxPeriod is the longest period of the event log in one export (D-090).
const MaxPeriod = 7 * 24 * time.Hour

// Service answers the export and the system log.
type Service struct {
	st     *store.Store
	log    *slog.Logger
	agents Agents
	mask   Masker

	mu      sync.Mutex
	running int // packages being built
}

// NewService returns the service; without a log it logs nothing.
func NewService(st *store.Store, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{st: st, log: log}
}

func period(from, to time.Time) error {
	if !to.After(from) || to.Sub(from) > MaxPeriod {
		return &users.InvalidError{Detail: "the period ends after it starts and is at most 7 days"}
	}
	return nil
}

// denied logs and audits a refusal for a role other than the RCON admin.
func (s *Service) denied(ctx context.Context, req users.Request, action string) error {
	perm.LogDenied(s.log, perm.Denial{
		RequestID: req.RequestID, SessionRef: req.SessionRef, User: req.Pseudonym,
		Role: req.Actor.Role, Reason: "forbidden", Action: action,
	})
	if _, err := audit.Write(ctx, s.st.DB, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		TargetType: "diagnostics", Action: action, Result: audit.ResultDenied, Reason: "forbidden",
	}); err != nil {
		s.log.Error("audit entry not written", slog.String("action", action), slog.Any("err", err))
	}
	return users.ErrForbidden
}
