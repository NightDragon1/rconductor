-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Versions for optimistic locking of objects saved as a whole (revision
-- 2.4 candidate 24): a write names the version it read and raises it in
-- one statement, UPDATE ... SET version = version + 1 WHERE id = ? AND
-- version = ?, so that it also holds with Postgres and several panels
-- (NFR-011). The triggers of migration 00010 raise it on every other
-- change, too.
ALTER TABLE user_groups ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE command_lists ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE servers ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE server_groups ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
-- The global settings are saved as a whole; one version for all.
CREATE TABLE settings_version (
    id      INTEGER NOT NULL PRIMARY KEY CHECK (id = 1),
    version INTEGER NOT NULL
);
INSERT INTO settings_version (id, version) VALUES (1, 1);

-- +goose Down
DROP TABLE settings_version;
ALTER TABLE server_groups DROP COLUMN version;
ALTER TABLE servers DROP COLUMN version;
ALTER TABLE command_lists DROP COLUMN version;
ALTER TABLE user_groups DROP COLUMN version;
