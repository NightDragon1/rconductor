// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package messages holds the messages in the panel (MSG-001 to MSG-003,
// D-085, D-086). Every operation checks the role of the caller and the
// audience and writes a refusal to the audit log.
package messages

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/events"
	"github.com/NightDragon1/rconductor/internal/foreign"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
	"github.com/NightDragon1/rconductor/internal/users"
)

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
	// notify gets the events of the topic messages; nil sends none.
	notify Notifier
}

// Notifier sends an event of the topic messages to the sockets of the
// given users (the WebSocket hub).
type Notifier interface {
	PublishMessage(userIDs []string, e events.MessageEvent)
}

// Notify sets where the events of the topic messages go.
func (s *Service) Notify(n Notifier) *Service {
	s.notify = n
	return s
}

// publish sends an event after the change is stored: what happened, never
// a text (D-085).
func (s *Service) publish(ids []string, kind, id, commentID string) {
	if s.notify != nil && len(ids) > 0 {
		s.notify.PublishMessage(ids, events.MessageEvent{Kind: kind, MessageID: id, CommentID: commentID})
	}
}

// participants are the recipients and the sender of a message.
func (s *Service) participants(ctx context.Context, id string) []string {
	ids, err := s.st.Queries.ListMessageParticipants(ctx, id)
	if err != nil {
		s.log.Warn("participants of a message not read", slog.Any("err", err))
	}
	return ids
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

// Page is one page of the caller's messages, newest first.
type Page struct {
	Items []Message
	// Unread counts the unread messages to the caller over all pages.
	Unread int
	// Next is the cursor of the next page; empty on the last one.
	Next string
}

// List lists the messages of the caller (D-085): those sent to him and
// those he sent; the RCON admin sees all. cursor is the Next of the page
// before: a position of time and ID, never looked up, so the next page
// follows also when messages of the page before are gone (MSG-001). Without
// a cursor, before (deprecated) is the ID of the last message of the page
// before, only compared with the caller's view.
func (s *Service) List(ctx context.Context, req users.Request, limit int, before, cursor string) (Page, error) {
	if err := s.read(ctx, req, "message.list", ""); err != nil {
		return Page{}, err
	}
	if limit < 1 || limit > maxPage {
		return Page{}, &users.InvalidError{Detail: "limit is 1 to 100"}
	}
	at, after := "", ""
	switch {
	case cursor != "":
		var err error
		if at, after, err = decodeCursor(cursor); err != nil {
			return Page{}, err
		}
	case before != "":
		cur, err := s.view(ctx, req, before, "", "", 1)
		if err != nil || len(cur) == 0 {
			return Page{Items: []Message{}}, err
		}
		at, after = cur[0].CreatedAt.Format(store.TimeLayout), before
	}
	items, err := s.view(ctx, req, "", at, after, limit+1)
	if err != nil {
		return Page{}, err
	}
	p := Page{Items: items}
	if len(items) > limit {
		p.Items = items[:limit]
		last := p.Items[limit-1]
		p.Next = encodeCursor(last.CreatedAt.Format(store.TimeLayout), last.ID)
	}
	unread, err := s.st.Queries.CountUnreadMessages(ctx, req.Actor.ID)
	if err != nil {
		return Page{}, err
	}
	p.Unread = int(unread)
	return p, nil
}

// Get returns one message to its recipients, its sender and the RCON
// admin; anyone else gets 404 as for an unknown message.
func (s *Service) Get(ctx context.Context, req users.Request, id string) (Message, error) {
	return s.one(ctx, req, "message.read", id)
}

// one checks the role, then reads one message of the caller's view.
func (s *Service) one(ctx context.Context, req users.Request, action, id string) (Message, error) {
	if err := s.read(ctx, req, action, id); err != nil {
		return Message{}, err
	}
	m, err := s.view(ctx, req, id, "", "", 1)
	if err != nil {
		return Message{}, err
	}
	if len(m) == 0 {
		// Unknown or not the caller's: 404 alike, logged and audited as a
		// refusal like any other (MSG-003).
		return Message{}, s.refused(ctx, req, action, id, "not_found", users.ErrNotFound, nil)
	}
	return m[0], nil
}

// view reads the caller's messages: one by id, or a page after the
// position (at, after).
func (s *Service) view(ctx context.Context, req users.Request, id, at, after string, limit int) ([]Message, error) {
	admin := 0
	if req.Actor.Role == perm.RCONAdmin {
		admin = 1
	}
	rows, err := s.st.Queries.ListMessagesView(ctx, sqlitedb.ListMessagesViewParams{
		UserID: req.Actor.ID, Column2: admin, Column3: at, ID: after, Column5: id, Limit: int64(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]Message, 0, len(rows))
	for _, r := range rows {
		created, err := time.Parse(store.TimeLayout, r.CreatedAt)
		if err != nil {
			return nil, err
		}
		m := Message{ID: r.ID, SenderID: r.SenderID.String, SenderPseudonym: r.SenderPseudonym, Audience: r.Audience,
			Text: r.Text, CreatedAt: created, RecipientCount: -1, CommentCount: int(r.Comments), Read: r.Unread == 0}
		if admin == 1 || r.SenderID.Valid && r.SenderID.String == req.Actor.ID {
			m.RecipientCount = int(r.Recipients)
		}
		out = append(out, m)
	}
	return out, nil
}

// Delete deletes a message with its recipients at once and for good:
// the sender and the RCON admin (D-085, D-086); a recipient gets 403,
// anyone else 404. The audit keeps only the metadata.
func (s *Service) Delete(ctx context.Context, req users.Request, id string) error {
	m, err := s.one(ctx, req, "message.deleted", id)
	if err != nil {
		return err
	}
	if req.Actor.Role != perm.RCONAdmin && m.SenderID != req.Actor.ID {
		return s.denied(ctx, req, "message.deleted", id)
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	ids, err := sqlitedb.New(tx).ListMessageParticipants(ctx, id)
	if err != nil {
		return err
	}
	if n, err := sqlitedb.New(tx).DeleteMessage(ctx, id); err != nil || n == 0 {
		if err == nil {
			err = users.ErrNotFound
		}
		return err
	}
	if _, err := audit.Write(ctx, tx, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		TargetType: "message", TargetID: id, Action: "message.deleted", Result: audit.ResultOK,
		// the metadata only: sender and number of recipients, never the text (D-086)
		Payload: map[string]any{"sender_id": m.SenderID, "recipients": m.RecipientCount},
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.publish(ids, "deleted", id, "")
	return nil
}

// MarkRead marks a message read for the caller; repeating it changes
// nothing. Sender and RCON admin who are not recipients get 403, anyone
// else 404.
func (s *Service) MarkRead(ctx context.Context, req users.Request, id string) error {
	if _, err := s.one(ctx, req, "message.mark_read", id); err != nil {
		return err
	}
	if _, err := s.st.Queries.MarkMessageRead(ctx, sqlitedb.MarkMessageReadParams{
		ReadAt: sql.NullString{String: s.now().Format(store.TimeLayout), Valid: true}, MessageID: id, UserID: req.Actor.ID,
	}); err != nil {
		return err
	}
	n, err := s.st.Queries.IsMessageRecipient(ctx, sqlitedb.IsMessageRecipientParams{MessageID: id, UserID: req.Actor.ID})
	if err != nil {
		return err
	}
	if n == 0 {
		if s.gone(ctx, req, id) {
			return users.ErrNotFound
		}
		return s.denied(ctx, req, "message.mark_read", id)
	}
	s.publish([]string{req.Actor.ID}, "read", id, "")
	return nil
}

// Comment is a comment on a message.
type Comment struct {
	ID, AuthorID, AuthorPseudonym, Text string
	CreatedAt                           time.Time
}

// Comments lists the comments of a message, oldest first, to everyone who
// sees the message (D-085). after is the ID of the last comment of the
// page before; it is only looked for among the comments of this message.
func (s *Service) Comments(ctx context.Context, req users.Request, id string, limit int, after, cursor string) ([]Comment, string, error) {
	if _, err := s.one(ctx, req, "message.comment_list", id); err != nil {
		return nil, "", err
	}
	if limit < 1 || limit > maxPage {
		return nil, "", &users.InvalidError{Detail: "limit is 1 to 100"}
	}
	at := ""
	switch {
	case cursor != "":
		var err error
		if at, after, err = decodeCursor(cursor); err != nil {
			return nil, "", err
		}
	case after != "":
		c, err := s.st.Queries.GetMessageComment(ctx, sqlitedb.GetMessageCommentParams{MessageID: id, ID: after})
		if errors.Is(err, sql.ErrNoRows) {
			return []Comment{}, "", nil
		} else if err != nil {
			return nil, "", err
		}
		at = c.CreatedAt
	}
	rows, err := s.st.Queries.ListMessageComments(ctx, sqlitedb.ListMessageCommentsParams{
		MessageID: id, Column2: at, ID: after, Limit: int64(limit + 1),
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = encodeCursor(rows[limit-1].CreatedAt, rows[limit-1].ID)
	}
	out := make([]Comment, 0, len(rows))
	for _, r := range rows {
		created, err := time.Parse(store.TimeLayout, r.CreatedAt)
		if err != nil {
			return nil, "", err
		}
		out = append(out, Comment{ID: r.ID, AuthorID: r.AuthorID.String, AuthorPseudonym: r.AuthorPseudonym, Text: r.Text, CreatedAt: created})
	}
	return out, next, nil
}

// Comment comments on a message: its recipients and its sender (D-085).
// The RCON admin who is neither gets 403, anyone else 404; then the input
// (400), then the count of the last hour (429). The audit keeps no text
// (D-086).
func (s *Service) Comment(ctx context.Context, req users.Request, id, text string) (Comment, error) {
	m, err := s.one(ctx, req, "message.commented", id)
	if err != nil {
		return Comment{}, err
	}
	if m.SenderID != req.Actor.ID {
		n, err := s.st.Queries.IsMessageRecipient(ctx, sqlitedb.IsMessageRecipientParams{MessageID: id, UserID: req.Actor.ID})
		if err != nil {
			return Comment{}, err
		}
		if n == 0 {
			if s.gone(ctx, req, id) {
				return Comment{}, users.ErrNotFound
			}
			return Comment{}, s.denied(ctx, req, "message.commented", id)
		}
	}
	text, _ = foreign.Text(text, len(text))
	switch {
	case foreign.Blank(text):
		return Comment{}, &users.InvalidError{Detail: "text is required"}
	case len(text) > MaxCommentBytes,
		utf8.RuneCountInString(text) > s.limit(ctx, users.SettingMessagesMaxComment, users.DefaultMessagesMaxComment, 1):
		return Comment{}, &users.InvalidError{Detail: "text is too long"}
	}
	perHour := s.limit(ctx, users.SettingCommentsPerHour, users.DefaultCommentsPerHour, 1)
	now := s.now()
	hourAgo := now.Add(-time.Hour).Format(store.TimeLayout)
	cid, err := store.NewID()
	if err != nil {
		return Comment{}, err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return Comment{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	sent, err := q.CountCommentSends(ctx, sqlitedb.CountCommentSendsParams{UserID: req.Actor.ID, SentAt: hourAgo})
	if err != nil {
		return Comment{}, err
	}
	if sent >= int64(perHour) {
		return Comment{}, ErrRateLimited
	}
	created := now.Format(store.TimeLayout)
	if err := q.InsertMessageComment(ctx, sqlitedb.InsertMessageCommentParams{
		ID: cid, MessageID: id, AuthorID: sql.NullString{String: req.Actor.ID, Valid: true},
		AuthorPseudonym: req.Pseudonym, Text: text, CreatedAt: created,
	}); err != nil {
		// The message went in the meantime: as for an unknown one.
		if strings.Contains(err.Error(), "FOREIGN KEY") {
			return Comment{}, users.ErrNotFound
		}
		return Comment{}, err
	}
	if err := q.DeleteOldCommentSends(ctx, sqlitedb.DeleteOldCommentSendsParams{UserID: req.Actor.ID, SentAt: hourAgo}); err != nil {
		return Comment{}, err
	}
	if err := q.InsertCommentSend(ctx, sqlitedb.InsertCommentSendParams{UserID: req.Actor.ID, SentAt: created}); err != nil {
		return Comment{}, err
	}
	if err := s.audited(ctx, tx, req, "message.commented", id, cid); err != nil {
		return Comment{}, err
	}
	if err := tx.Commit(); err != nil {
		return Comment{}, err
	}
	s.publish(s.participants(ctx, id), "comment", id, cid)
	return Comment{ID: cid, AuthorID: req.Actor.ID, AuthorPseudonym: req.Pseudonym, Text: text, CreatedAt: now}, nil
}

// DeleteComment deletes a comment at once and for good: its author, the
// sender of the message and the RCON admin (D-085, D-086); another
// recipient gets 403, anyone else 404, also for a comment of another
// message.
func (s *Service) DeleteComment(ctx context.Context, req users.Request, id, commentID string) error {
	m, err := s.one(ctx, req, "message.comment_deleted", id)
	if err != nil {
		return err
	}
	c, err := s.st.Queries.GetMessageComment(ctx, sqlitedb.GetMessageCommentParams{MessageID: id, ID: commentID})
	if errors.Is(err, sql.ErrNoRows) {
		return users.ErrNotFound
	} else if err != nil {
		return err
	}
	if req.Actor.Role != perm.RCONAdmin && m.SenderID != req.Actor.ID && (!c.AuthorID.Valid || c.AuthorID.String != req.Actor.ID) {
		return s.refused(ctx, req, "message.comment_deleted", id, "forbidden", users.ErrForbidden,
			map[string]any{"comment_id": commentID})
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if n, err := sqlitedb.New(tx).DeleteMessageComment(ctx, sqlitedb.DeleteMessageCommentParams{MessageID: id, ID: commentID}); err != nil || n == 0 {
		if err == nil {
			err = users.ErrNotFound
		}
		return err
	}
	if err := s.audited(ctx, tx, req, "message.comment_deleted", id, commentID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.publish(s.participants(ctx, id), "comment_deleted", id, commentID)
	return nil
}

// audited writes the audit entry of a comment: the message and the
// comment ID, never the text (D-086).
func (s *Service) audited(ctx context.Context, tx *sql.Tx, req users.Request, action, id, commentID string) error {
	_, err := audit.Write(ctx, tx, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		TargetType: "message", TargetID: id, Action: action, Result: audit.ResultOK,
		Payload: map[string]any{"comment_id": commentID},
	})
	return err
}

// Send sends a message: first the role and the audience (403), then the
// input (400), then the count of the last hour (429); what depends on the
// resolved audience (unknown user, no or too many recipients) comes after
// it and counts against the hour.
func (s *Service) Send(ctx context.Context, req users.Request, in Input) (Message, error) {
	allowed := false
	for _, a := range senders[req.Actor.Role] {
		allowed = allowed || a == in.Audience
	}
	if !allowed {
		return Message{}, s.denied(ctx, req, "message.sent", "")
	}
	// Control characters other than line ends and tabs go; a text of
	// space or invisible characters alone is none (SEC-006, MSG-001).
	in.Text, _ = foreign.Text(in.Text, len(in.Text))
	switch {
	case foreign.Blank(in.Text):
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
	m, to, err := s.store(ctx, req, in)
	if err == nil {
		s.publish(to, "message", m.ID, "")
	}
	return m, err
}

// recipients resolves the audience at sending time (D-085) through the
// sending transaction; the sender is never among them. It stops as soon as
// the audience has more than maxResolved users (MSG-001).
func (s *Service) recipients(ctx context.Context, q *sqlitedb.Queries, req users.Request, in Input, maxResolved int) ([]string, error) {
	var to []string
	if in.Audience == AudienceUsers {
		for _, id := range in.UserIDs {
			if _, err := q.GetActiveUserRole(ctx, id); errors.Is(err, sql.ErrNoRows) {
				return nil, &users.InvalidError{Detail: "user_ids names an unknown user"}
			} else if err != nil {
				return nil, err
			}
			if id != req.Actor.ID {
				to = append(to, id)
			}
		}
	} else {
		all, err := q.ListActiveUserRoles(ctx)
		if err != nil {
			return nil, err
		}
		var mine []string
		if in.Audience == AudienceScope || in.Audience == AudienceScopeModerators {
			r, err := s.perm.FreshTx(ctx, q, req.Actor.ID)
			if err != nil {
				return nil, err
			}
			mine = r.ServerIDs()
		}
		for _, u := range all {
			if u.ID == req.Actor.ID {
				continue
			}
			ok, err := s.receives(ctx, q, in.Audience, perm.Role(u.Role), u.ID, mine)
			if err != nil {
				return nil, err
			}
			if ok {
				to = append(to, u.ID)
			}
			if len(to) > maxResolved {
				return nil, ErrTooManyRecipients
			}
		}
	}
	if len(to) == 0 {
		return nil, &users.InvalidError{Detail: "the audience has no recipient"}
	}
	if len(to) > maxResolved {
		return nil, ErrTooManyRecipients
	}
	return to, nil
}

// receives reports whether a user of the given role belongs to the
// audience; for a scope, by a server shared with the sender's scope.
func (s *Service) receives(ctx context.Context, q *sqlitedb.Queries, audience string, role perm.Role, id string, scope []string) (bool, error) {
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
		r, err := s.perm.FreshTx(ctx, q, id)
		if err != nil {
			return false, err
		}
		return slices.ContainsFunc(r.ServerIDs(), func(sv string) bool { return slices.Contains(scope, sv) }), nil
	}
	return false, nil
}

// store counts the sends of the last hour (429), resolves the audience and
// writes the message, its recipients and the send in one transaction. A
// send refused by its audience counts against the hour too (MSG-001). The
// audit keeps the audience and the number of recipients, never the text
// (D-086).
func (s *Service) store(ctx context.Context, req users.Request, in Input) (Message, []string, error) {
	perHour := s.limit(ctx, users.SettingMessagesPerHour, users.DefaultMessagesPerHour, 1)
	maxResolved := s.limit(ctx, users.SettingMessagesMaxResolved, users.DefaultMessagesMaxResolved, 1)
	now := s.now()
	hourAgo := now.Add(-time.Hour).Format(store.TimeLayout)
	id, err := store.NewID()
	if err != nil {
		return Message{}, nil, err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return Message{}, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	sent, err := q.CountMessageSends(ctx, sqlitedb.CountMessageSendsParams{UserID: req.Actor.ID, SentAt: hourAgo})
	if err != nil {
		return Message{}, nil, err
	}
	if sent >= int64(perHour) {
		return Message{}, nil, ErrRateLimited
	}
	// A sender removed since the request was taken sends nothing and
	// leaves no count behind (MSG-001).
	if _, err := q.GetActiveUserRole(ctx, req.Actor.ID); errors.Is(err, sql.ErrNoRows) {
		return Message{}, nil, users.ErrForbidden
	} else if err != nil {
		return Message{}, nil, err
	}
	created := now.Format(store.TimeLayout)
	if err := q.DeleteOldMessageSends(ctx, sqlitedb.DeleteOldMessageSendsParams{UserID: req.Actor.ID, SentAt: hourAgo}); err != nil {
		return Message{}, nil, err
	}
	if err := q.InsertMessageSend(ctx, sqlitedb.InsertMessageSendParams{UserID: req.Actor.ID, SentAt: created}); err != nil {
		return Message{}, nil, err
	}
	to, err := s.recipients(ctx, q, req, in, maxResolved)
	if err != nil {
		var invalid *users.InvalidError
		if errors.Is(err, ErrTooManyRecipients) || errors.As(err, &invalid) {
			if cerr := tx.Commit(); cerr != nil {
				return Message{}, nil, cerr
			}
		}
		return Message{}, nil, err
	}
	if err := q.InsertMessage(ctx, sqlitedb.InsertMessageParams{
		ID: id, SenderID: sql.NullString{String: req.Actor.ID, Valid: true}, SenderPseudonym: req.Pseudonym,
		Audience: in.Audience, Text: in.Text, CreatedAt: created,
	}); err != nil {
		return Message{}, nil, err
	}
	for _, u := range to {
		if err := q.InsertMessageRecipient(ctx, sqlitedb.InsertMessageRecipientParams{MessageID: id, UserID: u}); err != nil {
			return Message{}, nil, err
		}
	}
	if _, err := audit.Write(ctx, tx, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		TargetType: "message", TargetID: id, Action: "message.sent", Result: audit.ResultOK,
		Payload: map[string]any{"audience": in.Audience, "recipients": len(to)},
	}); err != nil {
		return Message{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return Message{}, nil, err
	}
	return Message{ID: id, SenderID: req.Actor.ID, SenderPseudonym: req.Pseudonym, Audience: in.Audience, Text: in.Text,
		CreatedAt: now, RecipientCount: len(to), Read: true}, to, nil
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

// read refuses a role that is no reader; target is the message asked for,
// if any, so the audit names it (MSG-003).
func (s *Service) read(ctx context.Context, req users.Request, action, target string) error {
	for _, r := range readers {
		if req.Actor.Role == r {
			return nil
		}
	}
	return s.denied(ctx, req, action, target)
}

func (s *Service) denied(ctx context.Context, req users.Request, action, target string) error {
	return s.refused(ctx, req, action, target, "forbidden", users.ErrForbidden, nil)
}

// refused logs and audits a refusal with its reason and returns err; the
// payload names what the target does not, such as a comment (MSG-003).
func (s *Service) refused(ctx context.Context, req users.Request, action, target, reason string, err error, payload map[string]any) error {
	perm.LogDenied(s.log, perm.Denial{
		RequestID: req.RequestID, SessionRef: req.SessionRef, User: req.Pseudonym,
		Role: req.Actor.Role, Reason: reason, Action: action,
	})
	if _, werr := audit.Write(ctx, s.st.DB, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		TargetType: "message", TargetID: target, Action: action, Result: audit.ResultDenied, Reason: reason,
		Payload: payload,
	}); werr != nil {
		s.log.Error("audit entry not written", slog.String("action", action), slog.Any("err", werr))
	}
	return err
}

// maxCursor bounds a cursor and maxPage a page as the contract does.
const (
	maxCursor = 256
	maxPage   = 100
)

// encodeCursor makes the opaque position after an item: its time and ID.
func encodeCursor(at, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at + "|" + id))
}

// decodeCursor reads a cursor; anything that is not one is invalid (400).
// The position is only compared with the caller's view, never looked up.
func decodeCursor(c string) (at, id string, err error) {
	bad := &users.InvalidError{Detail: "cursor is not one of this list"}
	if len(c) > maxCursor {
		return "", "", bad
	}
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return "", "", bad
	}
	at, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return "", "", bad
	}
	if _, err := time.Parse(store.TimeLayout, at); err != nil {
		return "", "", bad
	}
	if u, err := uuid.Parse(id); err != nil || u.String() != id {
		return "", "", bad
	}
	return at, id, nil
}

// gone reports a message deleted after the caller's first look at it: the
// caller then gets 404 as for an unknown message, not a refusal (MSG-003).
func (s *Service) gone(ctx context.Context, req users.Request, id string) bool {
	m, err := s.view(ctx, req, id, "", "", 1)
	return err == nil && len(m) == 0
}
