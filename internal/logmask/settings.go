// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package logmask

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/users"
)

// ErrNotImplemented answers an operation that passed its checks but is not built yet.
var ErrNotImplemented = errors.New("not implemented yet")

// Settings answers the masking settings of the RCON admin (UI-022, D-088
// c). Skeleton while the API is built contract first: role, audited
// refusal, input, otherwise ErrNotImplemented.
type Settings struct {
	st  *store.Store
	log *slog.Logger
}

// NewSettings returns the settings service; without a log it logs nothing.
func NewSettings(st *store.Store, log *slog.Logger) *Settings {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Settings{st: st, log: log}
}

// SettingsInput is a change; nil fields stay, SetTexts replaces all texts.
// IfMatch is the version read before, nil without If-Match.
type SettingsInput struct {
	IfMatch   *int64
	Enabled   *bool
	SetTexts  bool
	Texts     []Text
	MaxTexts  *int
	TextRunes *int
}

// Get returns the setup.
func (s *Settings) Get(ctx context.Context, req users.Request) error {
	if req.Actor.Role != perm.RCONAdmin {
		return s.denied(ctx, req, "log_masking.read")
	}
	return ErrNotImplemented
}

// Update changes the setup: the role (403), the version (428), then the
// input (400), as in D-051 and D-058. Errors name a text by its position only (SEC-012).
func (s *Settings) Update(ctx context.Context, req users.Request, in SettingsInput) error {
	if req.Actor.Role != perm.RCONAdmin {
		return s.denied(ctx, req, "log_masking.updated")
	}
	if in.IfMatch == nil {
		return users.ErrPreconditionRequired
	}
	lim := Limits{Texts: MaxTexts, TextRunes: MaxTextRunes}
	if in.MaxTexts != nil {
		if *in.MaxTexts < 1 || *in.MaxTexts > MaxTexts {
			return &users.InvalidError{Detail: fmt.Sprintf("max_texts must be 1 to %d", MaxTexts)}
		}
		lim.Texts = *in.MaxTexts
	}
	if in.TextRunes != nil {
		if *in.TextRunes < MinTextRunes || *in.TextRunes > MaxTextRunes {
			return &users.InvalidError{Detail: fmt.Sprintf("max_text_chars must be %d to %d", MinTextRunes, MaxTextRunes)}
		}
		lim.TextRunes = *in.TextRunes
	}
	if _, err := New(in.Texts, lim); in.SetTexts && err != nil {
		return &users.InvalidError{Detail: err.Error()}
	}
	return ErrNotImplemented
}

func (s *Settings) denied(ctx context.Context, req users.Request, action string) error {
	perm.LogDenied(s.log, perm.Denial{
		RequestID: req.RequestID, SessionRef: req.SessionRef, User: req.Pseudonym,
		Role: req.Actor.Role, Reason: "forbidden", Action: action,
	})
	if _, err := audit.Write(ctx, s.st.DB, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		TargetType: "settings", TargetID: "log_masking", Action: action, Result: audit.ResultDenied, Reason: "forbidden",
	}); err != nil {
		s.log.Error("audit entry not written", slog.String("action", action), slog.Any("err", err))
	}
	return users.ErrForbidden
}
