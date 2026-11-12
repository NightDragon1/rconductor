// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"slices"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/NightDragon1/rconductor/migrations"
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

// TestO1MigrationEntriesInScopedAudit covers AUD-004 for the entries of
// migration 00018: a server admin sees those about users he manages, like
// any entry about them, and not those about others; the RCON admin sees
// all.
func TestO1MigrationEntriesInScopedAudit(t *testing.T) {
	f := withMore(t)
	ctx := context.Background()
	f.exec(`INSERT INTO servers (id, name, created_at, updated_at, deleted_at) VALUES ('gone', 'gone', 't', 't', 't')`)
	f.exec(`INSERT INTO grants (id, user_group_id, user_id, server_id, granted_by, created_at) VALUES ('o-m', 'mg', ?, 'gone', ?, 't')`, f.ids["m"], f.ids["sa"])
	f.exec(`INSERT INTO admin_scopes (id, user_id, server_id, assigned_by, created_at) VALUES ('o-sa2', ?, 'gone', ?, 't')`, f.ids["sa2"], f.ids["admin"])
	p, err := goose.NewProvider(goose.DialectSQLite3, f.st.DB, migrations.SQLite(), goose.WithGoMigrations(migrations.SQLiteGo()...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 17); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatal(err)
	}
	seen := func(actor string) []string {
		t.Helper()
		es, _, err := f.svc.Audit(ctx, f.as(actor), AuditQuery{Kind: "admin", Limit: MaxAuditLimit})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range es {
			if e.Action == ActionAssignmentRemoved {
				if e.ActorID != "" || e.ActorRole != "" || e.SessionRef != "" {
					t.Errorf("entry %s has an actor: %+v", e.ID, e)
				}
				id, _ := e.Payload["id"].(string)
				out = append(out, id)
			}
		}
		slices.Sort(out)
		return out
	}
	if got := seen("sa"); !slices.Equal(got, []string{"o-m"}) {
		t.Errorf("server admin sees %v", got)
	}
	if got := seen("admin"); !slices.Equal(got, []string{"o-m", "o-sa2"}) {
		t.Errorf("RCON admin sees %v", got)
	}
}
