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
	}
}

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
