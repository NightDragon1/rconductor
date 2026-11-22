// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package messages

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/users"
)

// TestCursorPages covers the cursor of MSG-001 and MSG-003: a page has a
// next cursor only when more follow (none after exactly limit items), the
// next page follows also when the last item of the page before is gone,
// and a cursor is only a position: a broken one or one over 256
// characters is invalid, one from another list or view shows nothing
// outside the caller's own.
func TestCursorPages(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	var ids []string
	for i := range 4 {
		w.clock = w.clock.Add(time.Second)
		m, err := w.s.Send(ctx, w.sa, Input{Text: fmt.Sprint("m", i), Audience: AudienceScopeModerators})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, m.ID)
	}
	p1, err := w.s.List(ctx, w.mod1, 2, "", "")
	if err != nil || len(p1.Items) != 2 || p1.Next == "" {
		t.Fatalf("first page %+v %v", p1, err)
	}
	// The last item of the first page goes; the next page still follows.
	if err := w.s.Delete(ctx, w.sa, p1.Items[1].ID); err != nil {
		t.Fatal(err)
	}
	p2, err := w.s.List(ctx, w.mod1, 2, "", p1.Next)
	if err != nil || len(p2.Items) != 2 || p2.Items[0].ID != ids[1] || p2.Items[1].ID != ids[0] || p2.Next != "" {
		t.Errorf("second page after a delete %+v %v, want the two oldest and no next", p2, err)
	}
	// A stranger with the cursor of another view sees nothing of it.
	if p, err := w.s.List(ctx, w.mod2, 2, "", p1.Next); err != nil || len(p.Items) != 0 {
		t.Errorf("stranger with a foreign cursor: %+v %v", p, err)
	}
	var inv *users.InvalidError
	for _, bad := range []string{"%%%", "bm90LWEtY3Vyc29y", strings.Repeat("A", 257), encodeCursor("not a time", ids[0]), encodeCursor("2026-11-22T00:00:00.000Z", "x"),
		encodeCursor("2026-11-22T00:00:00.000Z", strings.ToUpper(ids[0])), encodeCursor("2026-11-22T00:00:00.000Z", "urn:uuid:"+ids[0])} {
		if _, err := w.s.List(ctx, w.mod1, 2, "", bad); !errors.As(err, &inv) {
			t.Errorf("cursor %.20q: %v, want invalid", bad, err)
		}
		if _, _, err := w.s.Comments(ctx, w.sa, ids[0], 2, "", bad); !errors.As(err, &inv) {
			t.Errorf("comment cursor %.20q: %v, want invalid", bad, err)
		}
	}
	// Comments: exactly limit items give no next; a cursor of another
	// message's list is only a position in this one.
	for i := range 2 {
		w.clock = w.clock.Add(time.Second)
		if _, err := w.s.Comment(ctx, w.mod1, ids[0], fmt.Sprint("c", i)); err != nil {
			t.Fatal(err)
		}
	}
	list, next, err := w.s.Comments(ctx, w.sa, ids[0], 2, "", "")
	if err != nil || len(list) != 2 || next != "" {
		t.Errorf("comments, exactly limit: %d %q %v", len(list), next, err)
	}
	list, next, err = w.s.Comments(ctx, w.sa, ids[0], 1, "", "")
	if err != nil || len(list) != 1 || next == "" {
		t.Fatalf("comments, first of two: %d %q %v", len(list), next, err)
	}
	if rest, _, err := w.s.Comments(ctx, w.sa, ids[0], 1, "", next); err != nil || len(rest) != 1 || rest[0].ID == list[0].ID {
		t.Errorf("comments, second page: %+v %v", rest, err)
	}
	// Another message with a comment later than the cursor: only its own
	// comments come, never those of the message the cursor came from.
	w.clock = w.clock.Add(time.Second)
	own, err := w.s.Comment(ctx, w.mod1, ids[1], "other")
	if err != nil {
		t.Fatal(err)
	}
	if other, _, err := w.s.Comments(ctx, w.sa, ids[1], 5, "", next); err != nil || len(other) != 1 || other[0].ID != own.ID {
		t.Errorf("a cursor of another message: %+v %v", other, err)
	}
	for _, limit := range []int{0, 101} {
		if _, err := w.s.List(ctx, w.mod1, limit, "", ""); !errors.As(err, &inv) {
			t.Errorf("limit %d: %v", limit, err)
		}
	}
}

// TestCursorSameTime: messages of the same time page by ID across a page
// boundary, none twice and none lost.
func TestCursorSameTime(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	seen := map[string]bool{}
	for i := range 5 {
		if _, err := w.s.Send(ctx, w.sa, Input{Text: fmt.Sprint("m", i), Audience: AudienceScopeModerators}); err != nil {
			t.Fatal(err)
		}
	}
	cursor := ""
	for range 5 {
		p, err := w.s.List(ctx, w.mod1, 2, "", cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range p.Items {
			if seen[m.ID] {
				t.Errorf("%s twice", m.ID)
			}
			seen[m.ID] = true
		}
		if cursor = p.Next; cursor == "" {
			break
		}
	}
	if len(seen) != 5 {
		t.Errorf("%d of 5 messages seen", len(seen))
	}
}

// TestTextCleaned covers SEC-006 and MSG-001 for messages and comments: a
// text of space alone is none, control characters other than line ends
// and tabs are dropped before it is stored.
func TestTextCleaned(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	var inv *users.InvalidError
	for _, blank := range []string{" ", "\n\t ", "\x00\x1b"} {
		if _, err := w.s.Send(ctx, w.sa, Input{Text: blank, Audience: AudienceScopeModerators}); !errors.As(err, &inv) {
			t.Errorf("message %q: %v, want invalid", blank, err)
		}
	}
	m, err := w.s.Send(ctx, w.sa, Input{Text: "hi\x1b[31m\x00 there\n\tok", Audience: AudienceScopeModerators})
	if err != nil || m.Text != "hi[31m there\n\tok" {
		t.Fatalf("message %q %v", m.Text, err)
	}
	if _, err := w.s.Comment(ctx, w.mod1, m.ID, "  \n "); !errors.As(err, &inv) {
		t.Errorf("blank comment: %v, want invalid", err)
	}
	c, err := w.s.Comment(ctx, w.mod1, m.ID, "a\x07b")
	if err != nil || c.Text != "ab" {
		t.Errorf("comment %q %v", c.Text, err)
	}
}

// TestCommentWhileDeleted: a comment on a message deleted at the same time
// is 404 as for an unknown message, never a fault.
func TestCommentWhileDeleted(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	for i := range 20 {
		m, err := w.s.Send(ctx, w.sa, Input{Text: fmt.Sprint("m", i), Audience: AudienceScopeModerators})
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- w.s.Delete(ctx, w.sa, m.ID) }()
		if _, err := w.s.Comment(ctx, w.mod1, m.ID, "late"); err != nil && !errors.Is(err, users.ErrNotFound) {
			t.Errorf("comment during the delete: %v", err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

// TestC346StrangerAudited: who reads a message that is not his gets 404 as
// for an unknown one, logged and audited as a refusal.
func TestC346StrangerAudited(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	m, err := w.s.Send(ctx, w.sa, Input{Text: "hi", Audience: AudienceScopeModerators})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.Get(ctx, w.mod2, m.ID); !errors.Is(err, users.ErrNotFound) {
		t.Fatalf("stranger = %v", err)
	}
	var n int
	if err := w.s.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE action = 'message.read' AND result = 'denied' AND reason_code = 'not_found' AND target_id = ?`, m.ID).Scan(&n); err != nil || n != 1 {
		t.Errorf("audited refusals = %d (%v), want 1", n, err)
	}
}

// TestC257CursorRest: a cursor wins over before; a stranger with a broken
// cursor on the comments of a message gets 404, not 400 (the rights come
// first); the next page of comments follows also when the last comment of
// the page before is gone.
func TestC257CursorRest(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	var ids []string
	for i := range 3 {
		w.clock = w.clock.Add(time.Second)
		m, err := w.s.Send(ctx, w.sa, Input{Text: fmt.Sprint("m", i), Audience: AudienceScopeModerators})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, m.ID)
	}
	p1, err := w.s.List(ctx, w.mod1, 1, "", "")
	if err != nil || p1.Next == "" {
		t.Fatalf("first page %+v %v", p1, err)
	}
	// before names the oldest message, the cursor the first page: the
	// cursor counts.
	p2, err := w.s.List(ctx, w.mod1, 1, ids[0], p1.Next)
	if err != nil || len(p2.Items) != 1 || p2.Items[0].ID != ids[1] {
		t.Errorf("cursor with before: %+v %v", p2, err)
	}
	if _, _, err := w.s.Comments(ctx, w.mod2, ids[2], 5, "", "%%%"); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("stranger with a broken cursor: %v, want not found", err)
	}
	var cs []Comment
	for i := range 3 {
		w.clock = w.clock.Add(time.Second)
		c, err := w.s.Comment(ctx, w.mod1, ids[2], fmt.Sprint("c", i))
		if err != nil {
			t.Fatal(err)
		}
		cs = append(cs, c)
	}
	first, next, err := w.s.Comments(ctx, w.sa, ids[2], 1, "", "")
	if err != nil || len(first) != 1 || next == "" {
		t.Fatalf("first comment page %v %q %v", first, next, err)
	}
	if err := w.s.DeleteComment(ctx, w.mod1, ids[2], first[0].ID); err != nil {
		t.Fatal(err)
	}
	rest, _, err := w.s.Comments(ctx, w.sa, ids[2], 5, "", next)
	if err != nil || len(rest) != 2 || rest[0].ID != cs[1].ID {
		t.Errorf("after a deleted comment: %+v %v", rest, err)
	}
}
