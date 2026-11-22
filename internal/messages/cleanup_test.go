// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package messages

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/users"
)

// TestC380RemovedSenderAudited (MSG-001): a sender removed, or given
// another role, after the request was taken is refused with 403, and the
// refusal is logged and audited.
func TestC380RemovedSenderAudited(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if _, err := w.s.st.DB.Exec(`UPDATE users SET role = 'moderator' WHERE id = ?`, w.sa.Actor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.Send(ctx, w.sa, Input{Text: "hi", Audience: AudienceScope}); !errors.Is(err, users.ErrForbidden) {
		t.Errorf("send after a role change = %v, want 403", err)
	}
	if _, err := w.s.st.DB.Exec(`UPDATE users SET deleted_at = 't' WHERE id = ?`, w.admin.Actor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.Send(ctx, w.admin, Input{Text: "hi", Audience: AudienceModerators}); !errors.Is(err, users.ErrForbidden) {
		t.Errorf("send after removal = %v, want 403", err)
	}
	if n := denials(t, w.s, "message.sent"); n != 2 {
		t.Errorf("%d audited refusals, want 2", n)
	}
}

// TestC380DeleteCommentAndMessage (MSG-003): deleting a comment while its
// message is deleted ends with both gone; each call succeeds or is 404.
func TestC380DeleteCommentAndMessage(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	for range 10 {
		m, err := w.s.Send(ctx, w.admin, Input{Text: "hi", Audience: AudienceModerators})
		if err != nil {
			t.Fatal(err)
		}
		c, err := w.s.Comment(ctx, w.mod1, m.ID, "x")
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var errComment, errMessage error
		wg.Go(func() { errComment = w.s.DeleteComment(ctx, w.mod1, m.ID, c.ID) })
		wg.Go(func() { errMessage = w.s.Delete(ctx, w.admin, m.ID) })
		wg.Wait()
		if errMessage != nil || (errComment != nil && !errors.Is(errComment, users.ErrNotFound)) {
			t.Fatalf("delete comment = %v, delete message = %v", errComment, errMessage)
		}
		var left int
		_ = w.s.st.DB.QueryRow(`SELECT count(*) FROM message_comments WHERE id = ?`, c.ID).Scan(&left)
		if left != 0 {
			t.Fatalf("comment left after both deletes")
		}
		w.clock = w.clock.Add(time.Hour) // a fresh hour for the send limit
	}
}

// TestC380SupporterAuthor (MSG-003, D-085): a supporter in the audience
// comments and deletes the own comment; a moderator may not delete it.
func TestC380SupporterAuthor(t *testing.T) {
	w := newWorld(t)
	w.grantSupporter(t)
	ctx := context.Background()
	m, err := w.s.Send(ctx, w.sa, Input{Text: "hi", Audience: AudienceScope})
	if err != nil {
		t.Fatal(err)
	}
	c, err := w.s.Comment(ctx, w.sup, m.ID, "from a supporter")
	if err != nil {
		t.Fatalf("supporter comment = %v", err)
	}
	if err := w.s.DeleteComment(ctx, w.mod1, m.ID, c.ID); err == nil {
		t.Error("a moderator deleted the comment of a supporter")
	}
	if err := w.s.DeleteComment(ctx, w.sup, m.ID, c.ID); err != nil {
		t.Errorf("supporter deletes the own comment = %v", err)
	}
}
