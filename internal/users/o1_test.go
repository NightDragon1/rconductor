// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"testing"
)

// TestO1DeleteGroupEndsGrants covers ROLE-004: deleting a moderator group
// removes its grants, to the group and to a member by name, in the same
// transaction with one audit entry each; the user's assignments no longer
// name the group, and grants of other groups stay.
func TestO1DeleteGroupEndsGrants(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	f.exec(`INSERT INTO grants (id, user_group_id, user_id, server_id, server_group_id, granted_by, created_at) VALUES
		('g-all', 'mg', NULL, 's2', NULL, ?, 't'), ('g-m', 'mg', ?, NULL, 'mc', ?, 't'), ('g-other', 'mg0', ?, 's1', NULL, ?, 't')`,
		f.ids["sa"], f.ids["m"], f.ids["sa"], f.ids["m"], f.ids["sa"])
	before := f.count(`SELECT count(*) FROM grants WHERE user_group_id = 'mg'`)
	if before < 2 {
		t.Fatalf("fixture: %d grants of mg", before)
	}
	if err := f.svc.DeleteGroup(ctx, f.as("sa"), "mg"); err != nil {
		t.Fatal(err)
	}
	if n := f.count(`SELECT count(*) FROM grants WHERE user_group_id = 'mg'`); n != 0 {
		t.Errorf("%d grants outlive their group", n)
	}
	if n := f.count(`SELECT count(*) FROM grants WHERE id = 'g-other'`); n != 1 {
		t.Error("the grant of another group went too")
	}
	if n := f.count(`SELECT count(*) FROM audit_log WHERE action = 'assignment.remove' AND result = 'ok'
		AND actor_id = ? AND json_extract(payload, '$.cause') = 'group.delete'
		AND json_extract(payload, '$.entry.group_id') = 'mg'`, f.ids["sa"]); n != before {
		t.Errorf("%d audit entries for %d removed grants", n, before)
	}
	if n := f.count(`SELECT count(*) FROM audit_log WHERE action = 'assignment.remove' AND json_extract(payload, '$.id') = 'g-m'
		AND target_type = 'user' AND target_id = ? AND server_id IS NULL
		AND json_extract(payload, '$.entry.server_group_id') = 'mc'`, f.ids["m"]); n != 1 {
		t.Error("the grant to the member by name is not audited on him")
	}
	if n := f.count(`SELECT count(*) FROM audit_log WHERE action = 'assignment.remove' AND json_extract(payload, '$.id') = 'g-all'
		AND target_type = 'user_group' AND target_id = 'mg' AND server_id = 's2'`); n != 1 {
		t.Error("the grant to the whole group is not audited on the group")
	}
	a, err := f.svc.Assignments(ctx, f.as("admin"), f.ids["m"])
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range a.Grants {
		if g.GroupID == "mg" {
			t.Errorf("assignments still name the deleted group: %+v", a.Grants)
		}
	}
}
