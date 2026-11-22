// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package messages

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/users"
)

// TestC242RefusalNamesTheMessage: a role that reads no messages, refused
// on one message, is audited with that message as target (MSG-003).
func TestC242RefusalNamesTheMessage(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	m, err := w.s.Send(ctx, w.admin, Input{Text: "hi", Audience: AudienceAll})
	if err != nil {
		t.Fatal(err)
	}
	stranger := request(t, w.s, perm.Role("guest"))
	for action, call := range map[string]func() error{
		"message.read":      func() error { _, err := w.s.Get(ctx, stranger, m.ID); return err },
		"message.deleted":   func() error { return w.s.Delete(ctx, stranger, m.ID) },
		"message.mark_read": func() error { return w.s.MarkRead(ctx, stranger, m.ID) },
	} {
		if err := call(); !errors.Is(err, users.ErrForbidden) {
			t.Fatalf("%s = %v", action, err)
		}
		var n int
		if err := w.s.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE action = ? AND result = 'denied' AND target_id = ?`,
			action, m.ID).Scan(&n); err != nil || n != 1 {
			t.Errorf("%s: audited with target = %d (%v), want 1", action, n, err)
		}
	}
}

// TestC243BlankText: a text or comment of space and invisible characters
// alone is missing (400); one visible character is enough (MSG-001,
// MSG-003).
func TestC243BlankText(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	for _, text := range []string{" \n\t", "\u200B\u200D\uFEFF", "\u202E\u2066 \u2069", "\u3164\u2800", "\uFE0F"} {
		var invalid *users.InvalidError
		if _, err := w.s.Send(ctx, w.admin, Input{Text: text, Audience: AudienceAll}); !errors.As(err, &invalid) {
			t.Errorf("send %q = %v, want 400", text, err)
		}
	}
	m, err := w.s.Send(ctx, w.admin, Input{Text: "\u200Bx", Audience: AudienceAll})
	if err != nil {
		t.Fatal(err)
	}
	var invalid *users.InvalidError
	if _, err := w.s.Comment(ctx, w.mod1, m.ID, "\u200B \u2060"); !errors.As(err, &invalid) {
		t.Errorf("comment = %v, want 400", err)
	}
}

// TestC263ResolveBoundedAndCounted: an audience past
// messages.max_resolved_recipients is refused and the refusal counts
// against messages.per_hour, so a sender cannot resolve it again and
// again (MSG-001).
func TestC263ResolveBoundedAndCounted(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.set(t, users.SettingMessagesMaxResolved, 1)
	w.set(t, users.SettingMessagesPerHour, 2)
	in := Input{Text: "hi", Audience: AudienceAll}
	for range 2 {
		if _, err := w.s.Send(ctx, w.admin, in); !errors.Is(err, ErrTooManyRecipients) {
			t.Fatalf("send = %v, want too many recipients", err)
		}
	}
	if _, err := w.s.Send(ctx, w.admin, in); !errors.Is(err, ErrRateLimited) {
		t.Errorf("third = %v, want the limit of the hour", err)
	}
	var n int
	if err := w.s.st.DB.QueryRow(`SELECT count(*) FROM messages`).Scan(&n); err != nil || n != 0 {
		t.Errorf("messages = %d (%v), want 0", n, err)
	}
}

// TestC265SendsOfDeletedUser: deleting a user removes his counts of the
// last hour (MSG-001, MSG-003).
func TestC265SendsOfDeletedUser(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	m, err := w.s.Send(ctx, w.sa, Input{Text: "hi", Audience: AudienceScope})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.Comment(ctx, w.sa, m.ID, "c"); err != nil {
		t.Fatal(err)
	}
	us := users.NewService(w.s.st, w.s.perm, nil, nil)
	if err := us.Delete(ctx, w.admin, w.sa.Actor.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"message_sends", "comment_sends"} {
		var n int
		if err := w.s.st.DB.QueryRow(`SELECT count(*) FROM `+table+` WHERE user_id = ?`, w.sa.Actor.ID).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s = %d (%v), want 0", table, n, err)
		}
	}
}

// TestC274DeletedAuditMetadata: the audit of a deleted message keeps the
// sender and the number of recipients, never the text (D-086).
func TestC274DeletedAuditMetadata(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	m, err := w.s.Send(ctx, w.sa, Input{Text: "secret words", Audience: AudienceScope})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.s.Delete(ctx, w.admin, m.ID); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := w.s.st.DB.QueryRow(`SELECT payload FROM audit_log WHERE action = 'message.deleted' AND result = 'ok' AND target_id = ?`, m.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var p struct {
		SenderID   string `json:"sender_id"`
		Recipients int    `json:"recipients"`
		Text       *string
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if p.SenderID != w.sa.Actor.ID || p.Recipients != 1 || p.Text != nil {
		t.Errorf("payload = %s, want sender %s and 1 recipient", raw, w.sa.Actor.ID)
	}
}

// TestC297DeleteCommentRefusalNamesComment: another recipient refused on
// deleting a comment is audited with the comment ID (MSG-003).
func TestC297DeleteCommentRefusalNamesComment(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	m, err := w.s.Send(ctx, w.admin, Input{Text: "hi", Audience: AudienceModerators})
	if err != nil {
		t.Fatal(err)
	}
	c, err := w.s.Comment(ctx, w.mod1, m.ID, "mine")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.s.DeleteComment(ctx, w.mod2, m.ID, c.ID); !errors.Is(err, users.ErrForbidden) {
		t.Fatalf("delete = %v, want 403", err)
	}
	var n int
	if err := w.s.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE action = 'message.comment_deleted' AND result = 'denied'
		AND target_id = ? AND json_extract(payload, '$.comment_id') = ?`, m.ID, c.ID).Scan(&n); err != nil || n != 1 {
		t.Errorf("audited with comment = %d (%v), want 1", n, err)
	}
}
