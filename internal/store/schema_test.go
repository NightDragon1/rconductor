// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/NightDragon1/rconductor/migrations"
)

// migrated returns a store with all embedded migrations applied.
func migrated(t *testing.T) *Store {
	t.Helper()
	s := openStore(t, filepath.Join(t.TempDir(), "data"))
	var logs syncBuffer
	if _, err := s.Migrate(context.Background(), testLogger(&logs), MigrateOptions{}); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestIdentitySchema checks the constraints of migration 00002 that the
// permission model relies on (Design 4.1 to 4.3).
func TestIdentitySchema(t *testing.T) {
	s := migrated(t)
	ctx := context.Background()
	exec := func(q string, args ...any) error {
		_, err := s.DB.ExecContext(ctx, q, args...)
		return err
	}
	const user = `INSERT INTO users (id, pseudonym, role, password_hash, is_emergency, created_at, updated_at, deleted_at)
		VALUES (?, ?, ?, 'h', ?, 't', 't', ?)`
	mustOK := func(name string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	mustFail := func(name string, err error) {
		t.Helper()
		if err == nil {
			t.Errorf("%s: accepted, want constraint error", name)
		}
	}

	mustOK("admin", exec(user, "u1", "admin", "rcon_admin", 1, nil))
	mustOK("moderator", exec(user, "u2", "Mod", "moderator", 0, nil))
	mustFail("unknown role", exec(user, "u3", "x", "player", 0, nil))
	mustFail("pseudonym unique ignoring case", exec(user, "u4", "MOD", "moderator", 0, nil))
	mustOK("pseudonym of a deleted user is free again", exec(user, "u5", "old", "moderator", 0, "t"))
	mustOK("reuse deleted pseudonym", exec(user, "u6", "OLD", "moderator", 0, nil))
	mustFail("second emergency admin", exec(user, "u7", "admin2", "rcon_admin", 1, nil))
	mustFail("emergency admin must be rcon_admin", exec(user, "u8", "em", "moderator", 1, nil))

	mustOK("server", exec(`INSERT INTO servers (id, name, created_at, updated_at) VALUES ('s1', 'mc', 't', 't')`))
	mustOK("server group", exec(`INSERT INTO server_groups (id, name, created_at, updated_at) VALUES ('g1', 'Minecraft', 't', 't')`))
	mustFail("admin scope with server and group",
		exec(`INSERT INTO admin_scopes (id, user_id, server_group_id, server_id, assigned_by, created_at) VALUES ('a1', 'u2', 'g1', 's1', 'u1', 't')`))
	mustFail("admin scope without target",
		exec(`INSERT INTO admin_scopes (id, user_id, assigned_by, created_at) VALUES ('a2', 'u2', 'u1', 't')`))

	mustOK("command list", exec(`INSERT INTO command_lists (id, name, owner_id, created_at, updated_at) VALUES ('l1', 'basic', 'u1', 't', 't')`))
	mustFail("general group with command list",
		exec(`INSERT INTO user_groups (id, name, kind, owner_id, command_list_id, created_at, updated_at) VALUES ('ug0', 'x', 'general', 'u1', 'l1', 't', 't')`))
	mustOK("moderator group",
		exec(`INSERT INTO user_groups (id, name, kind, owner_id, command_list_id, created_at, updated_at) VALUES ('ug1', 'mods', 'moderator', 'u1', 'l1', 't', 't')`))
	mustOK("grant to group", exec(`INSERT INTO grants (id, user_group_id, server_id, granted_by, created_at) VALUES ('gr1', 'ug1', 's1', 'u1', 't')`))
	mustOK("direct grant names the group", exec(`INSERT INTO grants (id, user_group_id, user_id, server_id, granted_by, created_at) VALUES ('gr2', 'ug1', 'u2', 's1', 'u1', 't')`))
	mustFail("duplicate grant", exec(`INSERT INTO grants (id, user_group_id, server_id, granted_by, created_at) VALUES ('gr3', 'ug1', 's1', 'u1', 't')`))
	mustFail("grant without group", exec(`INSERT INTO grants (id, user_id, server_id, granted_by, created_at) VALUES ('gr4', 'u2', 's1', 'u1', 't')`))

	mustFail("invite for rcon_admin",
		exec(`INSERT INTO invites (id, token_hash, created_by, role, created_at, expires_at) VALUES ('i1', 'h1', 'u1', 'rcon_admin', 't', 't')`))
	mustFail("audit with unknown result",
		exec(`INSERT INTO audit_log (id, ts, kind, action, result) VALUES ('au1', 't', 'auth', 'login', 'maybe')`))
}

// TestServerProfileSchema checks migration 00003 (Design 4.2, 7).
func TestServerProfileSchema(t *testing.T) {
	s := migrated(t)
	ctx := context.Background()
	exec := func(q string, args ...any) error {
		_, err := s.DB.ExecContext(ctx, q, args...)
		return err
	}
	const profile = `INSERT INTO game_profiles (id, slug, name, protocol, default_port, source_file, content_hash, imported_at)
		VALUES (?, ?, 'Minecraft', 'source-rcon', ?, 'builtin:minecraft-java.yaml', 'h', 't')`
	if err := exec(profile, "p1", "minecraft-java", 25575); err != nil {
		t.Fatal(err)
	}
	if exec(profile, "p2", "minecraft-java", 25575) == nil {
		t.Error("duplicate slug accepted")
	}
	if exec(profile, "p3", "bad-port", 70000) == nil {
		t.Error("port out of range accepted")
	}
	if err := exec(`INSERT INTO catalog_commands (id, profile_id, position, path) VALUES ('c1', 'p1', 0, 'list')`); err != nil {
		t.Fatal(err)
	}
	if exec(`INSERT INTO catalog_commands (id, profile_id, position, path) VALUES ('c2', 'p1', 1, 'list')`) == nil {
		t.Error("duplicate catalog path accepted")
	}
	if err := exec(`INSERT INTO servers (id, name, profile_id, host, port, secret, created_at, updated_at)
		VALUES ('s1', 'mc', 'p1', 'mc.lan', 25575, x'00', 't', 't')`); err != nil {
		t.Fatal(err)
	}
	var protocol string
	if err := s.DB.QueryRowContext(ctx, `SELECT protocol FROM servers WHERE id = 's1'`).Scan(&protocol); err != nil || protocol != "source-rcon" {
		t.Errorf("default protocol = %q, %v", protocol, err)
	}
	if exec(`INSERT INTO servers (id, name, profile_id, created_at, updated_at) VALUES ('s2', 'x', 'nope', 't', 't')`) == nil {
		t.Error("unknown profile accepted")
	}
	if err := exec(`DELETE FROM game_profiles WHERE id = 'p1'`); err == nil {
		t.Error("profile in use deleted")
	}
}

// TestServersAgentFK covers migration 00007 (revision 2.3): rebuilding
// servers keeps every row that depends on it, and deleting an agent sets
// agent_id and container_name of its servers to NULL.
func TestServersAgentFK(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "data"))
	ctx := context.Background()
	var logs syncBuffer
	// Up to 00006 only: the SQL files before the Go migration.
	upTo6 := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.SQLite(), ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() <= "00006_~" {
			b, _ := fs.ReadFile(migrations.SQLite(), e.Name())
			upTo6[e.Name()] = &fstest.MapFile{Data: b}
		}
	}
	if _, err := s.Migrate(ctx, testLogger(&logs), MigrateOptions{FS: upTo6}); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO users (id, pseudonym, role, password_hash, created_at, updated_at) VALUES ('u', 'admin', 'rcon_admin', 'h', 't', 't')`)
	exec(`INSERT INTO agents (id, name, public_key, fingerprint, status, created_at) VALUES ('a1', 'host', x'00', 'fp1', 'active', 't')`)
	exec(`INSERT INTO servers (id, name, created_at, updated_at, agent_id, container_name) VALUES ('s1', 'mc', 't', 't', 'a1', 'mc')`)
	exec(`INSERT INTO servers (id, name, created_at, updated_at, agent_id, container_name) VALUES ('s2', 'gone', 't', 't', 'nope', 'x')`)
	exec(`INSERT INTO server_groups (id, name, created_at, updated_at) VALUES ('g', 'g', 't', 't')`)
	exec(`INSERT INTO server_group_members (server_group_id, server_id) VALUES ('g', 's1')`)
	exec(`INSERT INTO admin_scopes (id, user_id, server_id, assigned_by, created_at) VALUES ('as', 'u', 's1', 'u', 't')`)

	res, err := s.Migrate(ctx, testLogger(&logs), MigrateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.To < 7 {
		t.Fatalf("migrated to %d", res.To)
	}
	count := func(q string) int {
		t.Helper()
		var n int
		if err := s.DB.QueryRowContext(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count(`SELECT COUNT(*) FROM server_group_members`) != 1 || count(`SELECT COUNT(*) FROM admin_scopes`) != 1 {
		t.Error("rows that depend on servers were lost in the rebuild")
	}
	if count(`SELECT COUNT(*) FROM servers WHERE id = 's2' AND agent_id IS NULL AND container_name IS NULL`) != 1 {
		t.Error("a dangling agent_id survived the migration")
	}
	if count(`SELECT COUNT(*) FROM pragma_foreign_key_list('servers') WHERE "table" = 'agents' AND on_delete = 'SET NULL'`) != 1 {
		t.Error("no foreign key on agents")
	}
	if count(`SELECT COUNT(*) FROM pragma_foreign_key_check`) != 0 {
		t.Error("foreign key violations after the migration")
	}
	exec(`DELETE FROM agents WHERE id = 'a1'`)
	if count(`SELECT COUNT(*) FROM servers WHERE id = 's1' AND agent_id IS NULL AND container_name IS NULL`) != 1 {
		t.Error("deleting the agent left agent_id or container_name")
	}
	if count(`SELECT COUNT(*) FROM server_group_members`) != 1 {
		t.Error("deleting the agent touched the memberships")
	}
}
