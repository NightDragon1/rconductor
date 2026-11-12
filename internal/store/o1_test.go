// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/NightDragon1/rconductor/migrations"
)

// TestO1OrphanAssignmentsMigration covers ROLE-003, ROLE-004 and ROLE-007:
// migration 00018 removes the scopes, grants and supporter grants that name
// a deleted server, server group or user group, with one audit entry each
// and no actor, and leaves every assignment on a live object alone. Many
// left-behind rows go in one run.
func TestO1OrphanAssignmentsMigration(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "data"))
	ctx := context.Background()
	p, err := goose.NewProvider(goose.DialectSQLite3, s.DB, migrations.SQLite(), goose.WithGoMigrations(migrations.SQLiteGo()...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 17); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err := s.DB.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	for _, u := range []struct{ id, role string }{{"admin", "rcon_admin"}, {"sa", "server_admin"}, {"m", "moderator"}, {"sup", "supporter"}} {
		exec(`INSERT INTO users (id, pseudonym, role, password_hash, created_at, updated_at) VALUES (?, ?, ?, 'x', 't', 't')`, u.id, u.id, u.role)
	}
	exec(`INSERT INTO servers (id, name, created_at, updated_at, deleted_at) VALUES ('live', 'live', 't', 't', NULL), ('dead', 'dead', 't', 't', 't')`)
	exec(`INSERT INTO server_groups (id, name, created_at, updated_at, deleted_at) VALUES ('sg', 'sg', 't', 't', NULL), ('sgdead', 'sgdead', 't', 't', 't')`)
	exec(`INSERT INTO user_groups (id, name, kind, owner_id, created_at, updated_at, deleted_at) VALUES
		('ug', 'ug', 'moderator', 'sa', 't', 't', NULL), ('ugdead', 'ugdead', 'moderator', 'sa', 't', 't', 't')`)
	exec(`INSERT INTO admin_scopes (id, user_id, server_id, server_group_id, assigned_by, created_at) VALUES
		('sc-live', 'sa', 'live', NULL, 'admin', 't'), ('sc-sg', 'sa', NULL, 'sg', 'admin', 't'),
		('sc-dead', 'sa', 'dead', NULL, 'admin', 't'), ('sc-sgdead', 'sa', NULL, 'sgdead', 'admin', 't')`)
	exec(`INSERT INTO grants (id, user_group_id, user_id, server_id, server_group_id, granted_by, created_at) VALUES
		('gr-live', 'ug', NULL, 'live', NULL, 'sa', 't'), ('gr-sg', 'ug', 'm', NULL, 'sg', 'sa', 't'),
		('gr-dead', 'ug', NULL, 'dead', NULL, 'sa', 't'), ('gr-sgdead', 'ug', 'm', NULL, 'sgdead', 'sa', 't'),
		('gr-ugdead', 'ugdead', NULL, 'live', NULL, 'sa', 't')`)
	exec(`INSERT INTO supporter_grants (id, supporter_id, moderator_id, server_id, granted_by, created_at) VALUES
		('sg-live', 'sup', 'm', 'live', 'm', 't'), ('sg-dead', 'sup', 'm', 'dead', 'm', 't')`)
	// Many server admins with a scope on the deleted server.
	const many = 300
	for i := range many {
		id := fmt.Sprintf("sa%03d", i)
		exec(`INSERT INTO users (id, pseudonym, role, password_hash, created_at, updated_at) VALUES (?, ?, 'server_admin', 'x', 't', 't')`, id, id)
		exec(`INSERT INTO admin_scopes (id, user_id, server_id, assigned_by, created_at) VALUES (?, ?, 'dead', 'admin', 't')`, "sc-"+id, id)
	}
	exec(`INSERT INTO audit_log (id, ts, kind, action, result) VALUES ('before', 't', 'admin', 'server.delete', 'ok')`)

	if _, err := p.Up(ctx); err != nil {
		t.Fatal(err)
	}

	left := func(table string) string {
		t.Helper()
		var ids string
		if err := s.DB.QueryRowContext(ctx, "SELECT coalesce(group_concat(id, ','), '') FROM (SELECT id FROM "+table+" WHERE id NOT LIKE 'sc-sa%' ORDER BY id)").Scan(&ids); err != nil {
			t.Fatal(err)
		}
		return ids
	}
	if got := left("admin_scopes"); got != "sc-live,sc-sg" {
		t.Errorf("scopes left = %s", got)
	}
	if n := count(`SELECT count(*) FROM admin_scopes WHERE id LIKE 'sc-sa%'`); n != 0 {
		t.Errorf("%d of %d scopes on the deleted server left", n, many)
	}
	if got := left("grants"); got != "gr-live,gr-sg" {
		t.Errorf("grants left = %s", got)
	}
	if got := left("supporter_grants"); got != "sg-live" {
		t.Errorf("supporter grants left = %s", got)
	}

	const removal = `SELECT count(*) FROM audit_log WHERE action = 'assignment.remove' AND kind = 'admin' AND result = 'ok'
		AND actor_id IS NULL AND actor_role IS NULL AND session_id IS NULL AND json_extract(payload, '$.cause') = 'target_deleted'`
	if n := count(removal); n != 6+many {
		t.Errorf("%d audit entries, want one per removed assignment (%d)", n, 6+many)
	}
	for _, c := range []struct {
		id, section, targetType, target, server, entryKey, entryValue string
	}{
		{"sc-dead", "admin_scopes", "user", "sa", "dead", "server_id", "dead"},
		{"sc-sgdead", "admin_scopes", "user", "sa", "", "server_group_id", "sgdead"},
		{"gr-dead", "grants", "user_group", "ug", "dead", "server_id", "dead"},
		{"gr-sgdead", "grants", "user", "m", "", "server_group_id", "sgdead"},
		{"gr-ugdead", "grants", "user_group", "ugdead", "live", "group_id", "ugdead"},
		{"sg-dead", "supporter_grants", "user", "sup", "dead", "moderator_id", "m"},
	} {
		if n := count(removal+` AND json_extract(payload, '$.id') = ? AND json_extract(payload, '$.section') = ?
			AND target_type = ? AND target_id = ? AND coalesce(server_id, '') = ?
			AND json_extract(payload, '$.entry.'||?) = ?`,
			c.id, c.section, c.targetType, c.target, c.server, c.entryKey, c.entryValue); n != 1 {
			t.Errorf("audit entry for %s: %d matching", c.id, n)
		}
	}
	if n := count(`SELECT count(*) FROM audit_log WHERE id = 'before'`); n != 1 {
		t.Error("an earlier audit entry went")
	}
	if n := count(`SELECT count(*) FROM audit_log WHERE action = 'assignment.remove' AND length(id) <> 36`); n != 0 {
		t.Errorf("%d audit entries without a UUID", n)
	}
	if count(`SELECT count(*) FROM pragma_foreign_key_check`) != 0 {
		t.Error("foreign key violations after the migration")
	}
	// Down keeps what is there.
	if _, err := p.DownTo(ctx, 17); err != nil {
		t.Fatal(err)
	}
	if got := left("admin_scopes"); got != "sc-live,sc-sg" {
		t.Errorf("scopes after down = %s", got)
	}
}
