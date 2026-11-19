// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package messages

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/users"
)

// TestCommentInput covers MSG-003: 404 and 403 come first, then the text
// (required, at most MaxCommentBytes bytes, then messages.max_comment
// characters), then messages.comments_per_hour (429).
func TestCommentInput(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	m, err := w.s.Send(ctx, w.sa, Input{Text: "hi", Audience: AudienceScopeModerators})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.Comment(ctx, w.mod2, m.ID, ""); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("stranger with empty text = %v, want not found first", err)
	}
	if _, err := w.s.Comment(ctx, w.admin, m.ID, ""); !errors.Is(err, users.ErrForbidden) {
		t.Errorf("RCON admin, no recipient, empty text = %v, want forbidden first", err)
	}
	var inv *users.InvalidError
	for _, text := range []string{"", strings.Repeat("a", MaxCommentBytes+1), strings.Repeat("é", 2001)} {
		if _, err := w.s.Comment(ctx, w.mod1, m.ID, text); !errors.As(err, &inv) {
			t.Errorf("%d bytes: %v", len(text), err)
		}
	}
	if _, err := w.s.Comment(ctx, w.mod1, m.ID, strings.Repeat("é", 2000)); err != nil {
		t.Errorf("at messages.max_comment: %v", err)
	}
	w.set(t, users.SettingMessagesMaxComment, 4096)
	if _, err := w.s.Comment(ctx, w.mod1, m.ID, strings.Repeat("é", MaxCommentBytes/2+1)); !errors.As(err, &inv) {
		t.Errorf("over 4 KiB below the setting: %v", err)
	}
	w.set(t, users.SettingCommentsPerHour, 2)
	if _, err := w.s.Comment(ctx, w.sa, m.ID, "two"); err != nil {
		t.Errorf("sender: %v", err)
	}
	if _, err := w.s.Comment(ctx, w.mod1, m.ID, "three"); err != nil {
		t.Errorf("second of mod1: %v", err)
	}
	if _, err := w.s.Comment(ctx, w.mod1, m.ID, ""); !errors.As(err, &inv) {
		t.Errorf("empty text over the count = %v, want invalid before 429", err)
	}
	if _, err := w.s.Comment(ctx, w.mod1, m.ID, "four"); !errors.Is(err, ErrRateLimited) {
		t.Errorf("over the count = %v", err)
	}
}

// TestCommentRights covers MSG-003 and D-085: recipients and the sender
// see and write comments, the RCON admin sees them; a comment is deleted
// by its author, the sender and the RCON admin; another recipient gets
// 403, anyone else 404, also for a comment of another message. The audit
// keeps no text (D-086).
func TestCommentRights(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	m, err := w.s.Send(ctx, w.sa, Input{Text: "hi", Audience: AudienceScopeModerators})
	if err != nil {
		t.Fatal(err)
	}
	other, err := w.s.Send(ctx, w.admin, Input{Text: "other", Audience: AudienceUsers, UserIDs: []string{w.mod1.Actor.ID, w.sup.Actor.ID}})
	if err != nil {
		t.Fatal(err)
	}
	oc, err := w.s.Comment(ctx, w.mod1, other.ID, "x")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.s.DeleteComment(ctx, w.sup, other.ID, oc.ID); !errors.Is(err, users.ErrForbidden) {
		t.Errorf("another recipient deletes = %v", err)
	}
	c, err := w.s.Comment(ctx, w.mod1, m.ID, "secret words")
	if err != nil {
		t.Fatal(err)
	}
	for _, who := range []users.Request{w.sa, w.admin, w.mod1} {
		if list, err := w.s.Comments(ctx, who, m.ID, 50, ""); err != nil || len(list) != 1 || list[0].Text != "secret words" || list[0].AuthorID != w.mod1.Actor.ID {
			t.Errorf("%s sees %v, %v", who.Pseudonym, list, err)
		}
	}
	if _, err := w.s.Comments(ctx, w.mod2, m.ID, 50, ""); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("stranger lists = %v", err)
	}
	if got, _ := w.s.Get(ctx, w.mod1, m.ID); got.CommentCount != 1 {
		t.Errorf("comment count = %d", got.CommentCount)
	}
	if err := w.s.DeleteComment(ctx, w.mod2, m.ID, c.ID); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("stranger deletes = %v", err)
	}
	if err := w.s.DeleteComment(ctx, w.admin, other.ID, c.ID); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("comment of another message = %v", err)
	}
	for _, who := range []users.Request{w.mod1, w.sa, w.admin} {
		c, err := w.s.Comment(ctx, w.mod1, m.ID, "more secret words")
		if err != nil {
			t.Fatal(err)
		}
		if err := w.s.DeleteComment(ctx, who, m.ID, c.ID); err != nil {
			t.Errorf("%s deletes = %v", who.Pseudonym, err)
		}
	}
	var left int
	var audit string
	_ = w.s.st.DB.QueryRow(`SELECT count(*) FROM message_comments`).Scan(&left)
	_ = w.s.st.DB.QueryRow(`SELECT group_concat(action || coalesce(payload, ''), '|') FROM audit_log WHERE action LIKE 'message.comment%' AND result = 'ok'`).Scan(&audit)
	if left != 2 || strings.Count(audit, "message.commented") != 5 || strings.Count(audit, "message.comment_deleted") != 3 || strings.Contains(audit, "secret") {
		t.Errorf("left %d, audit %s", left, audit)
	}
	if err := w.s.Delete(ctx, w.sa, m.ID); err != nil {
		t.Fatal(err)
	}
	if _ = w.s.st.DB.QueryRow(`SELECT count(*) FROM message_comments WHERE message_id = ?`, m.ID).Scan(&left); left != 0 {
		t.Errorf("comments left after deleting the message: %d", left)
	}
}

// TestCommentsPaging: oldest first; after is looked for only among the
// comments of the message, so a foreign ID gives an empty page.
func TestCommentsPaging(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	m, _ := w.s.Send(ctx, w.sa, Input{Text: "hi", Audience: AudienceScopeModerators})
	other, _ := w.s.Send(ctx, w.sa, Input{Text: "other", Audience: AudienceScopeModerators})
	for i := range 5 {
		if i%2 == 0 {
			w.clock = w.clock.Add(time.Second) // pairs share a time stamp
		}
		if _, err := w.s.Comment(ctx, w.mod1, m.ID, fmt.Sprint("c", i)); err != nil {
			t.Fatal(err)
		}
	}
	foreign, _ := w.s.Comment(ctx, w.mod1, other.ID, "x")
	var got []string
	for after := ""; ; {
		page, err := w.s.Comments(ctx, w.sa, m.ID, 2, after)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		for _, c := range page {
			got = append(got, c.Text)
		}
		after = page[len(page)-1].ID
	}
	if strings.Join(got, ",") != "c0,c1,c2,c3,c4" {
		t.Errorf("pages = %v", got)
	}
	if page, err := w.s.Comments(ctx, w.sa, m.ID, 50, foreign.ID); err != nil || len(page) != 0 {
		t.Errorf("foreign cursor = %v, %v", page, err)
	}
}

// TestCommentDeleteCommentAgain covers D-086: deleting a comment does not
// lower messages.comments_per_hour; of parallel comments past the count
// exactly the count passes.
func TestCommentDeleteCommentAgain(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	m, _ := w.s.Send(ctx, w.sa, Input{Text: "hi", Audience: AudienceScopeModerators})
	w.set(t, users.SettingCommentsPerHour, 3)
	c, err := w.s.Comment(ctx, w.mod1, m.ID, "one")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.s.DeleteComment(ctx, w.mod1, m.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	var ok, limited int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_, err := w.s.Comment(ctx, w.mod1, m.ID, "again")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrRateLimited):
				limited++
			default:
				t.Errorf("comment = %v", err)
			}
		})
	}
	wg.Wait()
	if ok != 2 || limited != 6 {
		t.Errorf("passed %d, limited %d, want 2 and 6", ok, limited)
	}
	w.clock = w.clock.Add(time.Hour + time.Second)
	if _, err := w.s.Comment(ctx, w.mod1, m.ID, "next hour"); err != nil {
		t.Errorf("after an hour: %v", err)
	}
}
