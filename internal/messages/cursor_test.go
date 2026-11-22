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
