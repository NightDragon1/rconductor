// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package messages holds the messages in the panel (MSG-001 to MSG-003,
// D-085, D-086). Every operation checks the role of the caller and the
// audience and writes a refusal to the audit log; operations not built yet
// answer ErrNotImplemented.
package messages

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
	"github.com/NightDragon1/rconductor/internal/users"
)

// ErrNotImplemented is the answer of an operation that passed its checks
// but is not built yet.
var ErrNotImplemented = errors.New("not implemented yet")

// ErrTooManyRecipients means the audience resolves to more users than
// messages.max_resolved_recipients; a message is never cut.
var ErrTooManyRecipients = errors.New("the audience has more users than messages.max_resolved_recipients")

// ErrRateLimited means the sender sent messages.per_hour messages in the last
// hour; deleting one does not lower the count.
var ErrRateLimited = errors.New("too many messages in the last hour")

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
	st   *store.Store
	perm *perm.Checker
	log  *slog.Logger
	now  func() time.Time
}

// NewService returns the service; without a log it logs nothing.
func NewService(st *store.Store, checker *perm.Checker, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{st: st, perm: checker, log: log, now: func() time.Time { return time.Now().UTC() }}
}

// Message is a message as the caller sees it.
type Message struct {
	ID, SenderID, SenderPseudonym, Audience, Text string
	CreatedAt                                     time.Time
	// RecipientCount is for the sender and the RCON admin only; -1 hides it.
	RecipientCount int
	CommentCount   int
	Read           bool
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

// Comments lists the comments of a message.
func (s *Service) Comments(ctx context.Context, req users.Request, _ string) error {
	return s.read(ctx, req, "message.comment_list")
}

// Comment comments on a message: first the role (403), then the input
// (400), as in D-051. Whether the caller is a recipient or the sender is
// decided on the message itself.
func (s *Service) Comment(ctx context.Context, req users.Request, _, text string) error {
	if err := s.read(ctx, req, "message.commented"); !errors.Is(err, ErrNotImplemented) {
		return err
	}
	switch {
	case text == "":
		return &users.InvalidError{Detail: "text is required"}
	case len(text) > MaxCommentBytes:
		return &users.InvalidError{Detail: "text is too long"}
	}
	return ErrNotImplemented
}

// DeleteComment deletes a comment of a message.
func (s *Service) DeleteComment(ctx context.Context, req users.Request, _, _ string) error {
	return s.read(ctx, req, "message.comment_deleted")
}

// Send sends a message: first the role and the audience (403), then the
// input (400), then the count of the last hour (429), as in D-051.
func (s *Service) Send(ctx context.Context, req users.Request, in Input) (Message, error) {
	allowed := false
	for _, a := range senders[req.Actor.Role] {
		allowed = allowed || a == in.Audience
	}
	if !allowed {
		return Message{}, s.denied(ctx, req, "message.sent")
	}
	switch {
	case in.Text == "":
		return Message{}, &users.InvalidError{Detail: "text is required"}
	case len(in.Text) > MaxTextBytes:
		return Message{}, &users.InvalidError{Detail: "text is too long"}
	case in.Audience == AudienceUsers && len(in.UserIDs) == 0:
		return Message{}, &users.InvalidError{Detail: "user_ids is required for audience users"}
	case in.Audience != AudienceUsers && in.UserIDs != nil:
		return Message{}, &users.InvalidError{Detail: "user_ids is only for audience users"}
	case len(in.UserIDs) > MaxUserIDs:
		return Message{}, &users.InvalidError{Detail: "too many user_ids"}
	}
	seen := make(map[string]bool, len(in.UserIDs))
	for _, id := range in.UserIDs {
		if seen[id] {
			return Message{}, &users.InvalidError{Detail: "user_ids repeats an entry"}
		}
		seen[id] = true
	}
	if utf8.RuneCountInString(in.Text) > s.limit(ctx, users.SettingMessagesMaxText, users.DefaultMessagesMaxText, users.MinMessagesMaxText) {
		return Message{}, &users.InvalidError{Detail: "text is too long"}
	}
	if len(in.UserIDs) > s.limit(ctx, users.SettingMessagesMaxRecipients, users.DefaultMessagesMaxRecipients, 1) {
		return Message{}, &users.InvalidError{Detail: "too many user_ids"}
	}
	to, err := s.recipients(ctx, req, in)
	if err != nil {
		return Message{}, err
	}
	return s.store(ctx, req, in, to)
}

// recipients resolves the audience at sending time (D-085); the sender is
// never among them.
func (s *Service) recipients(ctx context.Context, req users.Request, in Input) ([]string, error) {
	var to []string
	if in.Audience == AudienceUsers {
		for _, id := range in.UserIDs {
			if _, err := s.st.Queries.GetActiveUserRole(ctx, id); errors.Is(err, sql.ErrNoRows) {
				return nil, &users.InvalidError{Detail: "user_ids names an unknown user"}
			} else if err != nil {
				return nil, err
			}
			if id != req.Actor.ID {
				to = append(to, id)
			}
		}
	} else {
		all, err := s.st.Queries.ListActiveUserRoles(ctx)
		if err != nil {
			return nil, err
		}
		var mine []string
		if in.Audience == AudienceScope || in.Audience == AudienceScopeModerators {
			r, err := s.perm.For(ctx, req.Actor.ID)
			if err != nil {
				return nil, err
			}
			mine = r.ServerIDs()
		}
		for _, u := range all {
			if u.ID == req.Actor.ID {
				continue
			}
			ok, err := s.receives(ctx, in.Audience, perm.Role(u.Role), u.ID, mine)
			if err != nil {
				return nil, err
			}
			if ok {
				to = append(to, u.ID)
			}
		}
	}
	if len(to) == 0 {
		return nil, &users.InvalidError{Detail: "the audience has no recipient"}
	}
	if len(to) > s.limit(ctx, users.SettingMessagesMaxResolved, users.DefaultMessagesMaxResolved, 1) {
		return nil, ErrTooManyRecipients
	}
	return to, nil
}

// receives reports whether a user of the given role belongs to the
// audience; for a scope, by a server shared with the sender's scope.
func (s *Service) receives(ctx context.Context, audience string, role perm.Role, id string, scope []string) (bool, error) {
	switch audience {
	case AudienceAll:
		return true, nil
	case AudienceServerAdmins:
		return role == perm.ServerAdmin, nil
	case AudienceModerators:
		return role == perm.Moderator, nil
	case AudienceScope, AudienceScopeModerators:
		if role != perm.Moderator && (audience == AudienceScopeModerators || role != perm.Supporter) {
			return false, nil
		}
		r, err := s.perm.For(ctx, id)
		if err != nil {
			return false, err
		}
		return slices.ContainsFunc(r.ServerIDs(), func(sv string) bool { return slices.Contains(scope, sv) }), nil
	}
	return false, nil
}

// store writes the message, its recipients and the send in one
// transaction, after the count of the last hour (429); the audit keeps
// the audience and the number of recipients, never the text (D-086).
func (s *Service) store(ctx context.Context, req users.Request, in Input, to []string) (Message, error) {
	perHour := s.limit(ctx, users.SettingMessagesPerHour, users.DefaultMessagesPerHour, 1)
	now := s.now()
	hourAgo := now.Add(-time.Hour).Format(store.TimeLayout)
	id, err := store.NewID()
	if err != nil {
		return Message{}, err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return Message{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	sent, err := q.CountMessageSends(ctx, sqlitedb.CountMessageSendsParams{UserID: req.Actor.ID, SentAt: hourAgo})
	if err != nil {
		return Message{}, err
	}
	if sent >= int64(perHour) {
		return Message{}, ErrRateLimited
	}
	created := now.Format(store.TimeLayout)
	if err := q.InsertMessage(ctx, sqlitedb.InsertMessageParams{
		ID: id, SenderID: sql.NullString{String: req.Actor.ID, Valid: true}, SenderPseudonym: req.Pseudonym,
		Audience: in.Audience, Text: in.Text, CreatedAt: created,
	}); err != nil {
		return Message{}, err
	}
	for _, u := range to {
		if err := q.InsertMessageRecipient(ctx, sqlitedb.InsertMessageRecipientParams{MessageID: id, UserID: u}); err != nil {
			return Message{}, err
		}
	}
	if err := q.DeleteOldMessageSends(ctx, sqlitedb.DeleteOldMessageSendsParams{UserID: req.Actor.ID, SentAt: hourAgo}); err != nil {
		return Message{}, err
	}
	if err := q.InsertMessageSend(ctx, sqlitedb.InsertMessageSendParams{UserID: req.Actor.ID, SentAt: created}); err != nil {
		return Message{}, err
	}
	if _, err := audit.Write(ctx, tx, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		TargetType: "message", TargetID: id, Action: "message.sent", Result: audit.ResultOK,
		Payload: map[string]any{"audience": in.Audience, "recipients": len(to)},
	}); err != nil {
		return Message{}, err
	}
	if err := tx.Commit(); err != nil {
		return Message{}, err
	}
	return Message{ID: id, SenderID: req.Actor.ID, SenderPseudonym: req.Pseudonym, Audience: in.Audience, Text: in.Text,
		CreatedAt: now, RecipientCount: len(to), Read: true}, nil
}

// limit reads a number setting through the guarded read: missing, the
// default; unreadable, the value last read, else the default; below its
// minimum, the default (F-93).
func (s *Service) limit(ctx context.Context, key string, def, minimum int) int {
	n := def
	if err := s.st.GuardedSetting(ctx, s.log, key, &n); err != nil || n < minimum {
		return def
	}
	return n
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
