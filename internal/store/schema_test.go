// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"context"
	"path/filepath"
	"testing"
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
