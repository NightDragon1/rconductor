// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"errors"
	"testing"
)

// TestC442ManagedReadInTheLock (Design 3.6, ROLE-004): a user who leaves
// the caller's management between the check before a write and its
// transaction is not changed, deleted or given new assignments; each
// refusal is not found, as for an unknown user, and audited.
func TestC442ManagedReadInTheLock(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	leave := func() {
		f.exec(`DELETE FROM user_group_members WHERE user_group_id = 'mg' AND user_id = ?`, f.ids["m"])
	}
	back := func() {
		f.exec(`INSERT INTO user_group_members (user_group_id, user_id, added_by, created_at) VALUES ('mg', ?, ?, 't')`, f.ids["m"], f.ids["sa"])
	}
	f.svc.beforeTx = leave
	off := true
	if _, err := f.svc.Update(ctx, f.as("sa"), f.ids["m"], Update{Disabled: &off}); !errors.Is(err, ErrNotFound) {
		t.Errorf("update: %v", err)
	}
	if n := f.count(`SELECT count(*) FROM users WHERE id = ? AND disabled_at IS NOT NULL`, f.ids["m"]); n != 0 {
		t.Error("the user was locked")
	}
	back()
	if err := f.svc.Delete(ctx, f.as("sa"), f.ids["m"]); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete: %v", err)
	}
	if n := f.count(`SELECT count(*) FROM users WHERE id = ? AND deleted_at IS NULL`, f.ids["m"]); n != 1 {
		t.Error("the user was deleted")
	}
	back()
	if _, err := f.svc.UpdateAssignments(ctx, f.as("sa"), f.ids["m"], AssignmentsUpdate{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("assignments: %v", err)
	}
	if n := f.count(`SELECT count(*) FROM audit_log WHERE target_id = ? AND result = 'denied' AND reason_code = 'not_found'`, f.ids["m"]); n != 3 {
		t.Errorf("%d refusals audited, want 3", n)
	}
}
