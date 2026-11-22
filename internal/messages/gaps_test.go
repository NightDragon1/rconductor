// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package messages

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/users"
)

// grantSupporter lets the supporter help mod1 on s1.
func (w *world) grantSupporter(t *testing.T) {
	t.Helper()
	if _, err := w.s.st.DB.Exec(`INSERT INTO supporter_grants (id, supporter_id, moderator_id, server_id, granted_by, created_at) VALUES ('sg1', ?, ?, 's1', ?, 't')`,
		w.sup.Actor.ID, w.mod1.Actor.ID, w.mod1.Actor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.st.DB.Exec(`UPDATE users SET supporters_allowed = 1 WHERE id = ?`, w.mod1.Actor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.st.DB.Exec(`INSERT INTO settings (id, key, value, scope_type, scope_id, updated_at) VALUES ('se', ?, 'true', 'global', '', 't')
		ON CONFLICT DO UPDATE SET value = excluded.value`, perm.SettingSupportersEnabled); err != nil {
		t.Fatal(err)
	}
	// the moderator's group needs a command list for s1 to count
	if _, err := w.s.st.DB.Exec(`INSERT INTO command_lists (id, name, owner_id, created_at, updated_at) VALUES ('l1', 'l1', ?, 't', 't')`, w.sa.Actor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.st.DB.Exec(`INSERT INTO command_list_entries (id, command_list_id, path, passable) VALUES ('e1', 'l1', 'list', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.st.DB.Exec(`UPDATE user_groups SET command_list_id = 'l1' WHERE id = 'g1'`); err != nil {
		t.Fatal(err)
	}
}

// TestResolutionCases covers MSG-001 and D-085: a supporter with a server
// in the scope receives scope but not scope_moderators; disabled and
// deleted users receive nothing; exactly messages.max_resolved_recipients
// passes, one more is refused.
func TestResolutionCases(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.grantSupporter(t)
	m, err := w.s.Send(ctx, w.sa, Input{Text: "hi", Audience: AudienceScope})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{w.mod1.Actor.ID, w.sup.Actor.ID}
	slices.Sort(want)
	if got := w.recipients(t, m.ID); !slices.Equal(got, want) {
		t.Errorf("scope = %v, want %v", got, want)
	}
	m, err = w.s.Send(ctx, w.sa, Input{Text: "hi", Audience: AudienceScopeModerators})
	if err != nil {
		t.Fatal(err)
	}
	if got := w.recipients(t, m.ID); !slices.Equal(got, []string{w.mod1.Actor.ID}) {
		t.Errorf("scope_moderators = %v", got)
	}
	// the supporter is no recipient of scope_moderators
	if _, err := w.s.Comment(ctx, w.sup, m.ID, "x"); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("supporter on scope_moderators = %v, want 404", err)
	}

	if _, err := w.s.st.DB.Exec(`UPDATE users SET disabled_at = 't' WHERE id = ?`, w.mod1.Actor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.st.DB.Exec(`UPDATE users SET deleted_at = 't' WHERE id = ?`, w.sup.Actor.ID); err != nil {
		t.Fatal(err)
	}
	w.s.perm.Invalidate()
	var invalid *users.InvalidError
	if _, err := w.s.Send(ctx, w.sa, Input{Text: "hi", Audience: AudienceScope}); !errors.As(err, &invalid) {
		t.Errorf("scope of disabled and deleted users = %v, want no recipient", err)
	}
	if _, err := w.s.Send(ctx, w.admin, Input{Text: "hi", Audience: AudienceUsers, UserIDs: []string{w.mod1.Actor.ID}}); !errors.As(err, &invalid) {
		t.Errorf("a disabled user named = %v, want 400", err)
	}

	// moderators: mod2 alone now; the bound at exactly one passes
	w.set(t, users.SettingMessagesMaxResolved, 1)
	if _, err := w.s.Send(ctx, w.admin, Input{Text: "hi", Audience: AudienceModerators}); err != nil {
		t.Errorf("at the bound = %v", err)
	}
	if _, err := w.s.Send(ctx, w.admin, Input{Text: "hi", Audience: AudienceAll}); !errors.Is(err, ErrTooManyRecipients) {
		t.Errorf("past the bound = %v", err)
	}
}

// TestRefusedAudienceCountsAndOrder: a send refused by its resolved
// audience counts against the hour; then 429 comes before that refusal
// (MSG-001).
func TestRefusedAudienceCountsAndOrder(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.set(t, users.SettingMessagesPerHour, 1)
	unknown := Input{Text: "hi", Audience: AudienceUsers, UserIDs: []string{"01900000-0000-7000-8000-000000000000"}}
	var invalid *users.InvalidError
	if _, err := w.s.Send(ctx, w.admin, unknown); !errors.As(err, &invalid) {
		t.Fatalf("unknown user = %v, want 400", err)
	}
	if _, err := w.s.Send(ctx, w.admin, Input{Text: "hi", Audience: AudienceModerators}); !errors.Is(err, ErrRateLimited) {
		t.Errorf("valid after a refused one = %v, want 429", err)
	}
	if _, err := w.s.Send(ctx, w.admin, unknown); !errors.Is(err, ErrRateLimited) {
		t.Errorf("unknown user over the limit = %v, want 429 first", err)
	}
}

// TestBrokenLimitsFallBack: an unreadable messages.per_hour or
// messages.comments_per_hour takes the default, never no limit and never
// zero (F-93).
func TestBrokenLimitsFallBack(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	for _, key := range []string{users.SettingMessagesPerHour, users.SettingCommentsPerHour} {
		if _, err := w.s.st.DB.Exec(`INSERT INTO settings (id, key, value, scope_type, scope_id, updated_at) VALUES (?, ?, 'broken', 'global', '', 't')`, key, key); err != nil {
			t.Fatal(err)
		}
	}
	var m Message
	var err error
	for i := range users.DefaultMessagesPerHour {
		if m, err = w.s.Send(ctx, w.admin, Input{Text: "hi", Audience: AudienceModerators}); err != nil {
			t.Fatalf("send %d = %v", i, err)
		}
	}
	if _, err := w.s.Send(ctx, w.admin, Input{Text: "hi", Audience: AudienceModerators}); !errors.Is(err, ErrRateLimited) {
		t.Errorf("past the default = %v, want 429", err)
	}
	for i := range users.DefaultCommentsPerHour {
		if _, err := w.s.Comment(ctx, w.mod1, m.ID, "c"); err != nil {
			t.Fatalf("comment %d = %v", i, err)
		}
	}
	if _, err := w.s.Comment(ctx, w.mod1, m.ID, "c"); !errors.Is(err, ErrRateLimited) {
		t.Errorf("comment past the default = %v, want 429", err)
	}
}

// TestParallelDeleteAndRead: deleting and marking read the same message
// at once, and deleting one comment twice at once: each ends as 204 or
// 404, never as 500, and the deletion holds (MSG-003).
func TestParallelDeleteAndRead(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	m, err := w.s.Send(ctx, w.admin, Input{Text: "hi", Audience: AudienceModerators})
	if err != nil {
		t.Fatal(err)
	}
	c, err := w.s.Comment(ctx, w.sup, m.ID, "x")
	if err == nil {
		t.Fatalf("a supporter outside the audience commented: %+v", c)
	}
	c, err = w.s.Comment(ctx, w.mod1, m.ID, "x")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 4)
	wg.Go(func() { errs[0] = w.s.DeleteComment(ctx, w.mod1, m.ID, c.ID) })
	wg.Go(func() { errs[1] = w.s.DeleteComment(ctx, w.admin, m.ID, c.ID) })
	wg.Wait()
	wg.Go(func() { errs[2] = w.s.Delete(ctx, w.admin, m.ID) })
	wg.Go(func() { errs[3] = w.s.MarkRead(ctx, w.mod1, m.ID) })
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Errorf("parallel comment deletes = %v, %v; want one 204, one 404", errs[0], errs[1])
	}
	for i, err := range errs {
		if err != nil && !errors.Is(err, users.ErrNotFound) {
			t.Errorf("%d = %v, want nil or 404", i, err)
		}
	}
	if errs[2] != nil {
		t.Errorf("delete = %v", errs[2])
	}
	if _, err := w.s.Get(ctx, w.mod1, m.ID); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("after delete = %v, want 404", err)
	}
}

// TestRemovedSenderLeavesNoCount: a sender removed after the request was
// taken sends nothing and leaves no count of the hour (MSG-001).
func TestRemovedSenderLeavesNoCount(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if _, err := w.s.st.DB.Exec(`UPDATE users SET deleted_at = 't' WHERE id = ?`, w.admin.Actor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.Send(ctx, w.admin, Input{Text: "hi", Audience: AudienceModerators}); !errors.Is(err, users.ErrForbidden) {
		t.Errorf("send = %v, want 403", err)
	}
	var n int
	if err := w.s.st.DB.QueryRow(`SELECT count(*) FROM message_sends WHERE user_id = ?`, w.admin.Actor.ID).Scan(&n); err != nil || n != 0 {
		t.Errorf("sends = %d (%v), want 0", n, err)
	}
}
