// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"slices"
	"sync"
	"testing"
)

// TestC255ServerAdminDeletes (D-106): a server admin deletes a moderator
// he manages; the moderator's general group goes with him, its members
// keep their sessions.
func TestC255ServerAdminDeletes(t *testing.T) {
	f := newFixture(t)
	f.exec(`INSERT INTO user_groups (id, name, kind, owner_id, created_at, updated_at) VALUES ('gg', 'general', 'general', ?, 't', 't')`, f.ids["m"])
	f.exec(`INSERT INTO user_group_members (user_group_id, user_id, added_by, created_at) VALUES ('gg', ?, ?, 't')`, f.ids["sup"], f.ids["m"])
	if err := f.svc.Delete(context.Background(), f.as("sa"), f.ids["m"]); err != nil {
		t.Fatalf("server admin deletes his moderator: %v", err)
	}
	if n := f.count(`SELECT count(*) FROM user_groups WHERE id = 'gg' AND deleted_at IS NOT NULL`); n != 1 {
		t.Error("the general group of the deleted user is still there")
	}
	if slices.Contains(f.revoked, f.ids["sup"]) {
		t.Errorf("a member of a general group lost the session: revoked %v", f.revoked)
	}
}

// TestC255ParallelDeletes (D-106): users deleted at the same time, as the
// interface does for a selection, all go, each with its own audit entry.
func TestC255ParallelDeletes(t *testing.T) {
	f := newFixture(t)
	ids := []string{f.ids["m"], f.ids["m2"], f.ids["sup"]}
	var wg sync.WaitGroup
	errs := make([]error, len(ids))
	for i, id := range ids {
		wg.Go(func() { errs[i] = f.svc.Delete(context.Background(), f.as("admin"), id) })
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("delete %d = %v", i, err)
		}
	}
	for _, id := range ids {
		if n := f.count(`SELECT count(*) FROM audit_log WHERE action = 'user.delete' AND target_id = ?`, id); n != 1 {
			t.Errorf("%d audit entries for %s, want 1", n, id)
		}
		if n := f.count(`SELECT count(*) FROM users WHERE id = ? AND deleted_at IS NOT NULL`, id); n != 1 {
			t.Errorf("%s not deleted", id)
		}
	}
}
