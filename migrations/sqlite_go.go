// SPDX-License-Identifier: AGPL-3.0-or-later

package migrations

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
)

// SQLiteGo returns the SQLite migrations written in Go: those that need
// one connection with foreign keys switched off, which an SQL file cannot
// guarantee (the pragma is per connection and ignored in a transaction).
func SQLiteGo() []*goose.Migration {
	return []*goose.Migration{
		goose.NewGoMigration(7,
			&goose.GoFunc{RunDB: serversAgentFK, Mode: goose.TransactionDisabled},
			&goose.GoFunc{RunDB: serversAgentFKDown, Mode: goose.TransactionDisabled}),
		goose.NewGoMigration(10,
			&goose.GoFunc{RunTx: execTx(versionTriggersUp)},
			&goose.GoFunc{RunTx: execTx(versionTriggersDown)}),
		goose.NewGoMigration(13,
			&goose.GoFunc{RunTx: execTx(secondFactorTriggersUp)},
			&goose.GoFunc{RunTx: execTx(secondFactorTriggersDown)}),
	}
}

func execTx(stmts string) func(context.Context, *sql.Tx) error {
	return func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, stmts)
		return err
	}
}

// 00010: every change of an object saved as a whole raises its version
// (revision 2.4 candidate 24): a change of the row itself when the write
// did not raise it already, and changes of its members, grants or entries.
// In Go because sqlc reads the SQL files as schema.
const versionTriggersUp = `
CREATE TRIGGER user_groups_version AFTER UPDATE ON user_groups WHEN NEW.version = OLD.version
BEGIN UPDATE user_groups SET version = version + 1 WHERE id = NEW.id; END;
CREATE TRIGGER command_lists_version AFTER UPDATE ON command_lists WHEN NEW.version = OLD.version
BEGIN UPDATE command_lists SET version = version + 1 WHERE id = NEW.id; END;
CREATE TRIGGER servers_version AFTER UPDATE ON servers WHEN NEW.version = OLD.version
BEGIN UPDATE servers SET version = version + 1 WHERE id = NEW.id; END;
CREATE TRIGGER server_groups_version AFTER UPDATE ON server_groups WHEN NEW.version = OLD.version
BEGIN UPDATE server_groups SET version = version + 1 WHERE id = NEW.id; END;

CREATE TRIGGER user_group_members_added AFTER INSERT ON user_group_members
BEGIN UPDATE user_groups SET version = version + 1 WHERE id = NEW.user_group_id; END;
CREATE TRIGGER user_group_members_removed AFTER DELETE ON user_group_members
BEGIN UPDATE user_groups SET version = version + 1 WHERE id = OLD.user_group_id; END;
CREATE TRIGGER group_grants_added AFTER INSERT ON grants WHEN NEW.user_id IS NULL
BEGIN UPDATE user_groups SET version = version + 1 WHERE id = NEW.user_group_id; END;
CREATE TRIGGER group_grants_removed AFTER DELETE ON grants WHEN OLD.user_id IS NULL
BEGIN UPDATE user_groups SET version = version + 1 WHERE id = OLD.user_group_id; END;

CREATE TRIGGER command_list_entries_added AFTER INSERT ON command_list_entries
BEGIN UPDATE command_lists SET version = version + 1 WHERE id = NEW.command_list_id; END;
CREATE TRIGGER command_list_entries_changed AFTER UPDATE ON command_list_entries
BEGIN UPDATE command_lists SET version = version + 1 WHERE id = NEW.command_list_id; END;
CREATE TRIGGER command_list_entries_removed AFTER DELETE ON command_list_entries
BEGIN UPDATE command_lists SET version = version + 1 WHERE id = OLD.command_list_id; END;

CREATE TRIGGER server_group_members_added AFTER INSERT ON server_group_members
BEGIN UPDATE server_groups SET version = version + 1 WHERE id = NEW.server_group_id; END;
CREATE TRIGGER server_group_members_removed AFTER DELETE ON server_group_members
BEGIN UPDATE server_groups SET version = version + 1 WHERE id = OLD.server_group_id; END;

CREATE TRIGGER settings_added AFTER INSERT ON settings WHEN NEW.scope_type = 'global'
BEGIN UPDATE settings_version SET version = version + 1 WHERE id = 1; END;
CREATE TRIGGER settings_changed AFTER UPDATE ON settings WHEN NEW.scope_type = 'global'
BEGIN UPDATE settings_version SET version = version + 1 WHERE id = 1; END;
CREATE TRIGGER settings_removed AFTER DELETE ON settings WHEN OLD.scope_type = 'global'
BEGIN UPDATE settings_version SET version = version + 1 WHERE id = 1; END;
`

const versionTriggersDown = `
DROP TRIGGER user_groups_version;
DROP TRIGGER command_lists_version;
DROP TRIGGER servers_version;
DROP TRIGGER server_groups_version;
DROP TRIGGER user_group_members_added;
DROP TRIGGER user_group_members_removed;
DROP TRIGGER group_grants_added;
DROP TRIGGER group_grants_removed;
DROP TRIGGER command_list_entries_added;
DROP TRIGGER command_list_entries_changed;
DROP TRIGGER command_list_entries_removed;
DROP TRIGGER server_group_members_added;
DROP TRIGGER server_group_members_removed;
DROP TRIGGER settings_added;
DROP TRIGGER settings_changed;
DROP TRIGGER settings_removed;
`

// 00007: servers.agent_id gets its foreign key on agents with ON DELETE
// SET NULL (revision 2.3); SQLite adds one only by building the table
// again. Foreign keys are off meanwhile, or dropping the old table would
// cascade into memberships, scopes, grants and supporter grants.
const serversAgentFKUp = `
UPDATE servers SET agent_id = NULL, container_name = NULL
    WHERE agent_id IS NOT NULL AND agent_id NOT IN (SELECT id FROM agents);
CREATE TABLE servers_new (
    id             TEXT NOT NULL PRIMARY KEY,
    name           TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    deleted_at     TEXT,
    profile_id     TEXT REFERENCES game_profiles (id),
    protocol       TEXT NOT NULL DEFAULT 'source-rcon',
    host           TEXT NOT NULL DEFAULT '',
    port           INTEGER NOT NULL DEFAULT 0 CHECK (port BETWEEN 0 AND 65535),
    secret         BLOB,
    agent_id       TEXT REFERENCES agents (id) ON DELETE SET NULL,
    container_name TEXT,
    game_ports     TEXT
);
INSERT INTO servers_new (id, name, created_at, updated_at, deleted_at, profile_id, protocol, host, port, secret, agent_id, container_name, game_ports)
    SELECT id, name, created_at, updated_at, deleted_at, profile_id, protocol, host, port, secret, agent_id, container_name, game_ports FROM servers;
DROP TABLE servers;
ALTER TABLE servers_new RENAME TO servers;
CREATE UNIQUE INDEX servers_name ON servers (name COLLATE NOCASE) WHERE deleted_at IS NULL;
CREATE INDEX servers_agent ON servers (agent_id);
-- Agent and container go together: losing the agent drops the container.
CREATE TRIGGER servers_agent_cleared AFTER UPDATE OF agent_id ON servers
    WHEN NEW.agent_id IS NULL AND NEW.container_name IS NOT NULL
BEGIN
    UPDATE servers SET container_name = NULL WHERE id = NEW.id;
END;
`

const serversAgentFKDownSQL = `
DROP TRIGGER servers_agent_cleared;
CREATE TABLE servers_old (
    id             TEXT NOT NULL PRIMARY KEY,
    name           TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    deleted_at     TEXT,
    profile_id     TEXT REFERENCES game_profiles (id),
    protocol       TEXT NOT NULL DEFAULT 'source-rcon',
    host           TEXT NOT NULL DEFAULT '',
    port           INTEGER NOT NULL DEFAULT 0 CHECK (port BETWEEN 0 AND 65535),
    secret         BLOB,
    agent_id       TEXT,
    container_name TEXT,
    game_ports     TEXT
);
INSERT INTO servers_old SELECT id, name, created_at, updated_at, deleted_at, profile_id, protocol, host, port, secret, agent_id, container_name, game_ports FROM servers;
DROP TABLE servers;
ALTER TABLE servers_old RENAME TO servers;
CREATE UNIQUE INDEX servers_name ON servers (name COLLATE NOCASE) WHERE deleted_at IS NULL;
`

func serversAgentFK(ctx context.Context, db *sql.DB) error {
	return withoutForeignKeys(ctx, db, serversAgentFKUp)
}

func serversAgentFKDown(ctx context.Context, db *sql.DB) error {
	return withoutForeignKeys(ctx, db, serversAgentFKDownSQL)
}

// withoutForeignKeys runs stmts in one transaction on one connection with
// foreign keys off, checks them all before the commit and switches them on
// again (SQLite's procedure for changing a table).
func withoutForeignKeys(ctx context.Context, db *sql.DB, stmts string) (err error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	defer func() {
		if _, ferr := conn.ExecContext(context.WithoutCancel(ctx), "PRAGMA foreign_keys = ON"); err == nil {
			err = ferr
		}
	}()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, stmts); err != nil {
		return err
	}
	violations, err := fkViolations(ctx, tx)
	if err != nil {
		return err
	}
	if len(violations) > 0 {
		return fmt.Errorf("foreign key check failed: %v", violations)
	}
	return tx.Commit()
}

// fkViolations lists the rows PRAGMA foreign_key_check reports.
func fkViolations(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var table, parent string
		var rowid sql.NullInt64
		var fk int
		if err := rows.Scan(&table, &rowid, &parent, &fk); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("%s row %d -> %s", table, rowid.Int64, parent))
	}
	return out, rows.Err()
}

// 00013: a waiting second step and a recovery link end when the password
// changes or the user is locked; a recovery link also when the second
// factor is reset or switched off (AUTH-008). In the database, so that no
// way of changing them is forgotten.
const secondFactorTriggersUp = `
CREATE TRIGGER users_password_ends_pending AFTER UPDATE OF password_hash ON users
    WHEN NEW.password_hash IS NOT OLD.password_hash
BEGIN
    DELETE FROM login_pending WHERE user_id = NEW.id;
    DELETE FROM totp_recovery WHERE user_id = NEW.id;
END;
CREATE TRIGGER users_lock_ends_pending AFTER UPDATE OF disabled_at ON users
    WHEN NEW.disabled_at IS NOT NULL
BEGIN
    DELETE FROM login_pending WHERE user_id = NEW.id;
    DELETE FROM totp_recovery WHERE user_id = NEW.id;
END;
CREATE TRIGGER users_totp_off_ends_recovery AFTER UPDATE OF totp_secret ON users
    WHEN NEW.totp_secret IS NULL AND OLD.totp_secret IS NOT NULL
BEGIN
    DELETE FROM totp_recovery WHERE user_id = NEW.id;
    DELETE FROM login_pending WHERE user_id = NEW.id;
END;
`

const secondFactorTriggersDown = `
DROP TRIGGER users_totp_off_ends_recovery;
DROP TRIGGER users_lock_ends_pending;
DROP TRIGGER users_password_ends_pending;
`
