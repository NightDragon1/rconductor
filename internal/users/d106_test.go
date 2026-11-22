// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
)

// TestD106DeleteUserWithGroupsAndLists: deleting a user deletes his user
// groups and command lists with him, in one transaction: the groups lose
// their grants, the members of his moderator groups have their rights
// recomputed, and the audit entry of the delete names what went. Groups
// and lists of other users stay.
func TestD106DeleteUserWithGroupsAndLists(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.exec(`INSERT INTO command_lists (id, name, owner_id, created_at, updated_at) VALUES ('l2', 'other list', ?, 't', 't')`, f.ids["sa2"])
	f.exec(`INSERT INTO user_groups (id, name, kind, owner_id, command_list_id, created_at, updated_at) VALUES ('og', 'other group', 'moderator', ?, 'l2', 't', 't')`, f.ids["sa2"])
	if err := f.svc.Delete(ctx, f.as("admin"), f.ids["sa"]); err != nil {
		t.Fatal(err)
	}
	if n := f.count(`SELECT count(*) FROM user_groups WHERE id = 'mg' AND deleted_at IS NOT NULL`); n != 1 {
		t.Error("the moderator group of the deleted user is still there")
	}
	if n := f.count(`SELECT count(*) FROM command_lists WHERE id = 'l1' AND deleted_at IS NOT NULL`); n != 1 {
		t.Error("the command list of the deleted user is still there")
	}
	if n := f.count(`SELECT count(*) FROM grants WHERE user_group_id = 'mg'`); n != 0 {
		t.Errorf("%d grants of the deleted group left", n)
	}
	if n := f.count(`SELECT count(*) FROM user_groups WHERE id = 'og' AND deleted_at IS NULL`) +
		f.count(`SELECT count(*) FROM command_lists WHERE id = 'l2' AND deleted_at IS NULL`); n != 2 {
		t.Error("group or list of another user deleted")
	}
	if !slices.Contains(f.revoked, f.ids["m"]) {
		t.Errorf("rights of the member m not recomputed: revoked %v", f.revoked)
	}
	var payload string
	if err := f.st.DB.QueryRow(`SELECT payload FROM audit_log WHERE action = 'user.delete' AND target_id = ?`, f.ids["sa"]).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var p struct {
		Groups       []struct{ ID, Name string } `json:"groups"`
		CommandLists []struct{ ID, Name string } `json:"command_lists"`
	}
	if err := json.Unmarshal([]byte(payload), &p); err != nil || len(p.Groups) != 1 || p.Groups[0].ID != "mg" ||
		len(p.CommandLists) != 1 || p.CommandLists[0].ID != "l1" {
		t.Errorf("audit payload %s", payload)
	}
	if n := f.count(`SELECT count(*) FROM audit_log WHERE action = ? AND payload LIKE '%"id":"g1"%' AND payload LIKE '%"cause":"user.delete"%'`, ActionAssignmentRemoved); n != 1 {
		t.Errorf("the removed grant is audited %d times, want once", n)
	}
}
