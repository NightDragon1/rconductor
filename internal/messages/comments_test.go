// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package messages

import (
	"context"
	"errors"
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

// TestCommentStored covers MSG-003 and D-086: the comment keeps author
// and text, the audit only the IDs; deleting the message deletes its
// comments.
func TestCommentStored(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	m, err := w.s.Send(ctx, w.sa, Input{Text: "hi", Audience: AudienceScopeModerators})
	if err != nil {
		t.Fatal(err)
	}
	c, err := w.s.Comment(ctx, w.mod1, m.ID, "secret words")
	if err != nil || c.AuthorID != w.mod1.Actor.ID || c.AuthorPseudonym != w.mod1.Pseudonym || c.Text != "secret words" {
		t.Fatalf("comment = %+v, %v", c, err)
	}
	var audit string
	_ = w.s.st.DB.QueryRow(`SELECT target_id || coalesce(payload, '') FROM audit_log WHERE action = 'message.commented' AND result = 'ok'`).Scan(&audit)
	if !strings.Contains(audit, m.ID) || !strings.Contains(audit, c.ID) || strings.Contains(audit, "secret") {
		t.Errorf("audit %s", audit)
	}
	if err := w.s.Delete(ctx, w.sa, m.ID); err != nil {
		t.Fatal(err)
	}
	var left int
	if _ = w.s.st.DB.QueryRow(`SELECT count(*) FROM message_comments`).Scan(&left); left != 0 {
		t.Errorf("comments left after deleting the message: %d", left)
	}
}

// TestCommentParallel: of parallel comments past
// messages.comments_per_hour exactly the count passes; an hour later the
// author comments again.
func TestCommentParallel(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	m, _ := w.s.Send(ctx, w.sa, Input{Text: "hi", Audience: AudienceScopeModerators})
	w.set(t, users.SettingCommentsPerHour, 2)
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
