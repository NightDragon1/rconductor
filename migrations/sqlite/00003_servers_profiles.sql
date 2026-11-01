-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Game profiles and the connection fields of servers (Design 4.2, 7; M2).

-- Imported game profiles; the files are the source (CAT-003). rules holds
-- syntax, ports, players and stop as JSON (Design 7.2). A custom profile in
-- /data/profiles with the same slug replaces the built-in one (overridden).
-- last_error keeps the reason of the last rejected import while the last
-- valid state stays active.
CREATE TABLE game_profiles (
    id            TEXT NOT NULL PRIMARY KEY,
    slug          TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    protocol      TEXT NOT NULL,
    default_port  INTEGER NOT NULL CHECK (default_port BETWEEN 1 AND 65535),
    rules         TEXT NOT NULL DEFAULT '{}',
    source_file   TEXT NOT NULL,
    content_hash  TEXT NOT NULL,
    overridden    INTEGER NOT NULL DEFAULT 0 CHECK (overridden IN (0, 1)),
    imported_at   TEXT NOT NULL,
    last_error    TEXT,
    last_error_at TEXT
);

-- Catalog commands of a profile (CAT-001); orientation only (CAT-002).
CREATE TABLE catalog_commands (
    id          TEXT NOT NULL PRIMARY KEY,
    profile_id  TEXT NOT NULL REFERENCES game_profiles (id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,
    path        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    params      TEXT NOT NULL DEFAULT '[]',
    category    TEXT,
    UNIQUE (profile_id, path)
);

-- secret is one value with key ID, nonce and ciphertext (Design 4.1,
-- SRV-002). agent_id and container_id get their foreign key with the
-- agents in M3. game_ports overrides the profile's game ports (SRV-005).
ALTER TABLE servers ADD COLUMN profile_id TEXT REFERENCES game_profiles (id);
ALTER TABLE servers ADD COLUMN protocol TEXT NOT NULL DEFAULT 'source-rcon';
ALTER TABLE servers ADD COLUMN host TEXT NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN port INTEGER NOT NULL DEFAULT 0 CHECK (port BETWEEN 0 AND 65535);
ALTER TABLE servers ADD COLUMN secret BLOB;
ALTER TABLE servers ADD COLUMN agent_id TEXT;
ALTER TABLE servers ADD COLUMN container_id TEXT;
ALTER TABLE servers ADD COLUMN game_ports TEXT;

-- +goose Down
ALTER TABLE servers DROP COLUMN game_ports;
ALTER TABLE servers DROP COLUMN container_id;
ALTER TABLE servers DROP COLUMN agent_id;
ALTER TABLE servers DROP COLUMN secret;
ALTER TABLE servers DROP COLUMN port;
ALTER TABLE servers DROP COLUMN host;
ALTER TABLE servers DROP COLUMN protocol;
ALTER TABLE servers DROP COLUMN profile_id;
DROP TABLE catalog_commands;
DROP TABLE game_profiles;
