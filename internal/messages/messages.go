// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package messages holds the messages in the panel (MSG-001 to MSG-003,
// D-085, D-086). This is its skeleton while the API is built contract first:
// every operation checks the role of the caller and the audience, writes a
// refusal to the audit log and otherwise answers ErrNotImplemented.
package messages

import (
	"context"
	"errors"
	"log/slog"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/users"
)

// ErrNotImplemented is the answer of an operation that passed its checks
// but is not built yet.
var ErrNotImplemented = errors.New("not implemented yet")

// Fixed bounds of the input, checked before the settings (field lengths).
const (
	MaxTextBytes    = 8 << 10 // the text in UTF-8, before it is counted in characters
	MaxCommentBytes = 4 << 10
	MaxUserIDs      = 500 // the upper end of messages.max_recipients
)

// Audiences of a message (D-085).
const (
	AudienceAll             = "all"
	AudienceServerAdmins    = "server_admins"
	AudienceModerators      = "moderators"
	AudienceUsers           = "users"
	AudienceScope           = "scope"
	AudienceScopeModerators = "scope_moderators"
)

// senders lists for each role the audiences it may send to; a role that is
// not listed sends nothing.
var senders = map[perm.Role][]string{
	perm.RCONAdmin:   {AudienceAll, AudienceServerAdmins, AudienceModerators, AudienceUsers},
	perm.ServerAdmin: {AudienceScope, AudienceScopeModerators},
}

// readers are the roles that can be recipients, senders or the RCON admin
// of a message; whether the caller is one of them for a given message is
// decided on the message itself.
var readers = []perm.Role{perm.RCONAdmin, perm.ServerAdmin, perm.Moderator, perm.Supporter}

// Service answers the message operations.
type Service struct {
	st  *store.Store
	log *slog.Logger
}

// NewService returns the service; without a log it logs nothing.
func NewService(st *store.Store, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{st: st, log: log}
}

// Input is a message to send.
type Input struct {
	Text     string
	Audience string
	UserIDs  []string
}

// List lists the messages of the caller.
func (s *Service) List(ctx context.Context, req users.Request) error {
	return s.read(ctx, req, "message.list")
}

// Get returns one message.
func (s *Service) Get(ctx context.Context, req users.Request, _ string) error {
	return s.read(ctx, req, "message.read")
}

// Delete deletes a message.
func (s *Service) Delete(ctx context.Context, req users.Request, _ string) error {
	return s.read(ctx, req, "message.delete")
}

// MarkRead marks a message read for the caller.
func (s *Service) MarkRead(ctx context.Context, req users.Request, _ string) error {
	return s.read(ctx, req, "message.mark_read")
}

// Send sends a message: first the role and the audience (403), then the
// input (400), as in D-051.
func (s *Service) Send(ctx context.Context, req users.Request, in Input) error {
	allowed := false
	for _, a := range senders[req.Actor.Role] {
		allowed = allowed || a == in.Audience
	}
	if !allowed {
		return s.denied(ctx, req, "message.send")
	}
	switch {
	case in.Text == "":
		return &users.InvalidError{Detail: "text is required"}
	case len(in.Text) > MaxTextBytes:
		return &users.InvalidError{Detail: "text is too long"}
	case in.Audience == AudienceUsers && len(in.UserIDs) == 0:
		return &users.InvalidError{Detail: "user_ids is required for audience users"}
	case in.Audience != AudienceUsers && in.UserIDs != nil:
		return &users.InvalidError{Detail: "user_ids is only for audience users"}
	case len(in.UserIDs) > MaxUserIDs:
		return &users.InvalidError{Detail: "too many user_ids"}
	}
	seen := make(map[string]bool, len(in.UserIDs))
	for _, id := range in.UserIDs {
		if seen[id] {
			return &users.InvalidError{Detail: "user_ids repeats an entry"}
		}
		seen[id] = true
	}
	return ErrNotImplemented
}

func (s *Service) read(ctx context.Context, req users.Request, action string) error {
	for _, r := range readers {
		if req.Actor.Role == r {
			return ErrNotImplemented
		}
	}
	return s.denied(ctx, req, action)
}

func (s *Service) denied(ctx context.Context, req users.Request, action string) error {
	perm.LogDenied(s.log, perm.Denial{
		RequestID: req.RequestID, SessionRef: req.SessionRef, User: req.Pseudonym,
		Role: req.Actor.Role, Reason: "forbidden", Action: action,
	})
	if _, err := audit.Write(ctx, s.st.DB, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		TargetType: "message", Action: action, Result: audit.ResultDenied, Reason: "forbidden",
	}); err != nil {
		s.log.Error("audit entry not written", slog.String("action", action), slog.Any("err", err))
	}
	return users.ErrForbidden
}
