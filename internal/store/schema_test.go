// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"

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
	if err := exec(`INSERT INTO servers (id, name, host, port, secret, created_at, updated_at)
		VALUES ('s1', 'mc', 'mc.lan', 25575, x'00', 't', 't')`); err != nil {
		t.Fatal(err)
	}
	if err := exec(`INSERT INTO server_profiles (server_id, profile_id, position) VALUES ('s1', 'p1', 0)`); err != nil {
		t.Fatal(err)
	}
	var protocol string
	if err := s.DB.QueryRowContext(ctx, `SELECT protocol FROM servers WHERE id = 's1'`).Scan(&protocol); err != nil || protocol != "source-rcon" {
		t.Errorf("default protocol = %q, %v", protocol, err)
	}
	if err := exec(`INSERT INTO servers (id, name, created_at, updated_at) VALUES ('s2', 'x', 't', 't')`); err != nil {
		t.Fatal(err)
	}
	if exec(`INSERT INTO server_profiles (server_id, profile_id, position) VALUES ('s2', 'nope', 0)`) == nil {
		t.Error("unknown profile accepted")
	}
	if exec(`INSERT INTO server_profiles (server_id, profile_id, position) VALUES ('s1', 'p1', 1)`) == nil {
		t.Error("a profile twice on a server accepted")
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

// TestServerProfilesMigration covers D-074: 00017 takes the one profile of
// a server of 1.0 as its only profile at position 0, keeps a server
// without profile without one, drops servers.profile_id, and goes back.
func TestServerProfilesMigration(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "data"))
	ctx := context.Background()
	p, err := goose.NewProvider(goose.DialectSQLite3, s.DB, migrations.SQLite(), goose.WithGoMigrations(migrations.SQLiteGo()...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 16); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO game_profiles (id, slug, name, protocol, default_port, rules, source_file, content_hash, imported_at)
		VALUES ('p1', 'mc', 'mc', 'source-rcon', 25575, '{}', 'f', 'h', 't')`)
	exec(`INSERT INTO servers (id, name, profile_id, created_at, updated_at) VALUES ('with', 'with', 'p1', 't', 't')`)
	exec(`INSERT INTO servers (id, name, created_at, updated_at) VALUES ('without', 'without', 't', 't')`)
	if _, err := p.Up(ctx); err != nil {
		t.Fatal(err)
	}
	count := func(q string) int {
		t.Helper()
		var n int
		if err := s.DB.QueryRowContext(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count(`SELECT COUNT(*) FROM server_profiles WHERE server_id = 'with' AND profile_id = 'p1' AND position = 0`) != 1 {
		t.Error("the profile of a 1.0 server is not its only profile at position 0")
	}
	if count(`SELECT COUNT(*) FROM server_profiles WHERE server_id = 'without'`) != 0 {
		t.Error("a server without profile got one")
	}
	if count(`SELECT COUNT(*) FROM pragma_table_info('servers') WHERE name = 'profile_id'`) != 0 {
		t.Error("servers.profile_id is still there")
	}
	if count(`SELECT COUNT(*) FROM pragma_foreign_key_check`) != 0 {
		t.Error("foreign key violations after the migration")
	}
	// Deleting a server takes its profile list with it.
	exec(`DELETE FROM servers WHERE id = 'with'`)
	if count(`SELECT COUNT(*) FROM server_profiles`) != 0 {
		t.Error("the profile list outlived its server")
	}
	exec(`INSERT INTO servers (id, name, created_at, updated_at) VALUES ('again', 'again', 't', 't')`)
	exec(`INSERT INTO server_profiles (server_id, profile_id, position) VALUES ('again', 'p1', 0)`)
	if _, err := p.DownTo(ctx, 16); err != nil {
		t.Fatal(err)
	}
	if count(`SELECT COUNT(*) FROM servers WHERE id = 'again' AND profile_id = 'p1'`) != 1 {
		t.Error("down did not bring the top profile back to servers.profile_id")
	}
}

// TestServerNumberMigration covers UI-018: 00019 numbers existing servers,
// deleted ones included, by creation time (ties by ID), sets the counter to
// the highest number, refuses a number twice, and goes back.
func TestServerNumberMigration(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "data"))
	ctx := context.Background()
	p, err := goose.NewProvider(goose.DialectSQLite3, s.DB, migrations.SQLite(), goose.WithGoMigrations(migrations.SQLiteGo()...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 18); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) error {
		_, err := s.DB.ExecContext(ctx, q, args...)
		return err
	}
	for _, r := range [][3]string{
		{"c", "2026-03-01T00:00:00Z", ""},
		{"a", "2026-01-01T00:00:00Z", "2026-02-01T00:00:00Z"}, // deleted, keeps its place
		{"b2", "2026-02-01T00:00:00Z", ""},
		{"b1", "2026-02-01T00:00:00Z", ""}, // same time: by ID
	} {
		if err := exec(`INSERT INTO servers (id, name, created_at, updated_at, deleted_at) VALUES (?, ?, ?, ?, NULLIF(?, ''))`, r[0], r[0], r[1], r[1], r[2]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"a": 1, "b1": 2, "b2": 3, "c": 4}
	for id, n := range want {
		var got int64
		if err := s.DB.QueryRowContext(ctx, `SELECT number FROM servers WHERE id = ?`, id).Scan(&got); err != nil || got != n {
			t.Errorf("server %s: number %d %v, want %d", id, got, err, n)
		}
	}
	var counter int64
	if err := s.DB.QueryRowContext(ctx, `SELECT value FROM counters WHERE name = 'server_number'`).Scan(&counter); err != nil || counter != 4 {
		t.Errorf("counter = %d %v, want 4", counter, err)
	}
	if err := exec(`INSERT INTO servers (id, number, name, created_at, updated_at) VALUES ('d', 4, 'd', 't', 't')`); err == nil {
		t.Error("a number was taken twice")
	}
	if _, err := p.DownTo(ctx, 18); err != nil {
		t.Fatal(err)
	}
	var cols int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('servers') WHERE name = 'number'`).Scan(&cols); err != nil || cols != 0 {
		t.Errorf("down left servers.number (%d, %v)", cols, err)
	}
}

// TestOneBindingMigration covers D-105: 00024 keeps the binding of a
// container at the oldest server not deleted (ties by ID), releases it at
// the others with a log line db.migrate.binding_released and an audit
// entry, leaves deleted servers and other containers alone, then refuses a
// second binding; down drops the index only.
func TestOneBindingMigration(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "data"))
	ctx := context.Background()
	p, err := goose.NewProvider(goose.DialectSQLite3, s.DB, migrations.SQLite(), goose.WithGoMigrations(migrations.SQLiteGo()...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 23); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) error {
		_, err := s.DB.ExecContext(ctx, q, args...)
		return err
	}
	if err := exec(`INSERT INTO agents (id, name, public_key, fingerprint, status, created_at) VALUES ('ag', 'nas', x'00', 'f', 'active', 't')`); err != nil {
		t.Fatal(err)
	}
	// A server whose agent was revoked keeps its container name only.
	if err := exec(`INSERT INTO servers (id, name, created_at, updated_at, container_name) VALUES ('detached', 'detached', 't', 't', 'mc')`); err != nil {
		t.Fatal(err)
	}
	for _, r := range [][4]string{
		{"b-new", "2026-03-01T00:00:00Z", "mc", ""},
		{"a-old", "2026-01-01T00:00:00Z", "mc", ""},
		{"a-tie", "2026-01-01T00:00:00Z", "mc", ""}, // same time as a-old: by ID it comes second
		{"gone", "2025-12-01T00:00:00Z", "mc", "2026-02-01T00:00:00Z"},
		{"other", "2026-03-01T00:00:00Z", "web", ""},
	} {
		if err := exec(`INSERT INTO servers (id, name, created_at, updated_at, agent_id, container_name, deleted_at) VALUES (?, ?, ?, ?, 'ag', ?, NULLIF(?, ''))`,
			r[0], r[0], r[1], r[1], r[2], r[3]); err != nil {
			t.Fatal(err)
		}
	}
	var logs syncBuffer
	if _, err := s.Migrate(ctx, testLogger(&logs), MigrateOptions{}); err != nil {
		t.Fatal(err)
	}
	bound := func(id string) bool {
		t.Helper()
		var agent sql.NullString
		if err := s.DB.QueryRowContext(ctx, `SELECT agent_id FROM servers WHERE id = ?`, id).Scan(&agent); err != nil {
			t.Fatal(err)
		}
		return agent.Valid
	}
	for id, want := range map[string]bool{"a-old": true, "a-tie": false, "b-new": false, "gone": true, "other": true} {
		if got := bound(id); got != want {
			t.Errorf("%s bound = %v, want %v", id, got, want)
		}
	}
	var audited int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM audit_log WHERE action = 'server.binding_released'
		AND target_id IN ('a-tie', 'b-new') AND payload LIKE '%"kept_by":"a-old"%'`).Scan(&audited); err != nil || audited != 2 {
		t.Errorf("audit entries = %d (%v), want 2", audited, err)
	}
	if n := strings.Count(logs.String(), `"event":"db.migrate.binding_released"`); n != 2 {
		t.Errorf("log lines = %d, want 2:\n%s", n, logs.String())
	}
	var name sql.NullString
	if err := s.DB.QueryRowContext(ctx, `SELECT container_name FROM servers WHERE id = 'detached'`).Scan(&name); err != nil || name.String != "mc" {
		t.Errorf("the detached server changed: %v %v", name, err)
	}
	if strings.Contains(logs.String(), "detached") {
		t.Error("the detached server was released")
	}
	if strings.Contains(logs.String(), `"mc"`) {
		t.Error("the log names the container")
	}
	if err := exec(`UPDATE servers SET agent_id = 'ag', container_name = 'mc' WHERE id = 'b-new'`); err == nil {
		t.Error("a second binding of the container was stored")
	}
	if err := exec(`UPDATE servers SET agent_id = 'ag', container_name = 'mc', deleted_at = 't' WHERE id = 'b-new'`); err != nil {
		t.Errorf("a deleted server keeps no container from others: %v", err)
	}
	if _, err := p.DownTo(ctx, 23); err != nil {
		t.Fatal(err)
	}
	if err := exec(`UPDATE servers SET agent_id = 'ag', container_name = 'mc', deleted_at = NULL WHERE id = 'b-new'`); err != nil {
		t.Errorf("down left the index: %v", err)
	}
}
