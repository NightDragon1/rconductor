-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Identity, rights and audit (Design 4.2, 4.3; M1). IDs are UUIDv7, times
-- are UTC text in store.TimeLayout. Names are unique only among records
-- that are not soft-deleted (Design 4.1).

-- Users are created only by redeeming an invite, apart from the emergency
-- admin. The avatar is stored in the database so a backup is complete.
CREATE TABLE users (
    id                 TEXT NOT NULL PRIMARY KEY,
    pseudonym          TEXT NOT NULL,
    real_name          TEXT,
    contacts           TEXT NOT NULL DEFAULT '{}',
    avatar             BLOB,
    avatar_type        TEXT,
    role               TEXT NOT NULL
        CHECK (role IN ('rcon_admin', 'server_admin', 'moderator', 'supporter')),
    auth_source        TEXT NOT NULL DEFAULT 'local' CHECK (auth_source IN ('local')),
    password_hash      TEXT NOT NULL,
    is_emergency       INTEGER NOT NULL DEFAULT 0 CHECK (is_emergency IN (0, 1)),
    supporters_allowed INTEGER NOT NULL DEFAULT 0 CHECK (supporters_allowed IN (0, 1)),
    created_by         TEXT REFERENCES users (id),
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    last_login_at      TEXT,
    disabled_at        TEXT,
    deleted_at         TEXT,
    CHECK ((avatar IS NULL) = (avatar_type IS NULL)),
    CHECK (is_emergency = 0 OR role = 'rcon_admin')
);
CREATE UNIQUE INDEX users_pseudonym ON users (pseudonym COLLATE NOCASE) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX users_emergency ON users (is_emergency) WHERE is_emergency = 1;

-- Session IDs are stored only as SHA-256 hash (Design 4.1).
CREATE TABLE sessions (
    id_hash      TEXT NOT NULL PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at   TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    ip           TEXT NOT NULL,
    user_agent   TEXT NOT NULL
);
CREATE INDEX sessions_user ON sessions (user_id);
CREATE INDEX sessions_expires ON sessions (expires_at);

CREATE TABLE invites (
    id               TEXT NOT NULL PRIMARY KEY,
    token_hash       TEXT NOT NULL UNIQUE,
    created_by       TEXT NOT NULL REFERENCES users (id),
    role             TEXT NOT NULL
        CHECK (role IN ('server_admin', 'moderator', 'supporter')),
    template_user_id TEXT REFERENCES users (id),
    created_at       TEXT NOT NULL,
    expires_at       TEXT NOT NULL,
    used_at          TEXT,
    used_by          TEXT REFERENCES users (id),
    CHECK ((used_at IS NULL) = (used_by IS NULL))
);
CREATE INDEX invites_expires ON invites (expires_at);

CREATE TABLE command_lists (
    id         TEXT NOT NULL PRIMARY KEY,
    name       TEXT NOT NULL,
    owner_id   TEXT NOT NULL REFERENCES users (id),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);
CREATE UNIQUE INDEX command_lists_name ON command_lists (owner_id, name COLLATE NOCASE) WHERE deleted_at IS NULL;

-- path is a token path such as "whitelist add" (CMD-004).
CREATE TABLE command_list_entries (
    id              TEXT NOT NULL PRIMARY KEY,
    command_list_id TEXT NOT NULL REFERENCES command_lists (id) ON DELETE CASCADE,
    path            TEXT NOT NULL,
    passable        INTEGER NOT NULL DEFAULT 0 CHECK (passable IN (0, 1)),
    UNIQUE (command_list_id, path)
);

-- kind general: plain user group; kind moderator: moderator group with its
-- command list (ROLE-005).
CREATE TABLE user_groups (
    id              TEXT NOT NULL PRIMARY KEY,
    name            TEXT NOT NULL,
    kind            TEXT NOT NULL CHECK (kind IN ('general', 'moderator')),
    owner_id        TEXT NOT NULL REFERENCES users (id),
    command_list_id TEXT REFERENCES command_lists (id),
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    deleted_at      TEXT,
    CHECK (kind = 'moderator' OR command_list_id IS NULL)
);
CREATE UNIQUE INDEX user_groups_name ON user_groups (owner_id, name COLLATE NOCASE) WHERE deleted_at IS NULL;

CREATE TABLE user_group_members (
    user_group_id TEXT NOT NULL REFERENCES user_groups (id) ON DELETE CASCADE,
    user_id       TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    added_by      TEXT NOT NULL REFERENCES users (id),
    created_at    TEXT NOT NULL,
    PRIMARY KEY (user_group_id, user_id)
);
CREATE INDEX user_group_members_user ON user_group_members (user_id);

-- Servers and server groups exist from M1 on for scopes and grants; the
-- connection fields and their API follow in M2 (SRV-001, SRV-003).
CREATE TABLE servers (
    id         TEXT NOT NULL PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);
CREATE UNIQUE INDEX servers_name ON servers (name COLLATE NOCASE) WHERE deleted_at IS NULL;

CREATE TABLE server_groups (
    id         TEXT NOT NULL PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);
CREATE UNIQUE INDEX server_groups_name ON server_groups (name COLLATE NOCASE) WHERE deleted_at IS NULL;

CREATE TABLE server_group_members (
    server_group_id TEXT NOT NULL REFERENCES server_groups (id) ON DELETE CASCADE,
    server_id       TEXT NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    PRIMARY KEY (server_group_id, server_id)
);
CREATE INDEX server_group_members_server ON server_group_members (server_id);

-- Scope of a server admin: exactly one of server group or server (ROLE-003).
CREATE TABLE admin_scopes (
    id              TEXT NOT NULL PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    server_group_id TEXT REFERENCES server_groups (id) ON DELETE CASCADE,
    server_id       TEXT REFERENCES servers (id) ON DELETE CASCADE,
    assigned_by     TEXT NOT NULL REFERENCES users (id),
    created_at      TEXT NOT NULL,
    CHECK ((server_group_id IS NULL) <> (server_id IS NULL))
);
CREATE UNIQUE INDEX admin_scopes_unique ON admin_scopes (user_id, coalesce(server_group_id, ''), coalesce(server_id, ''));

-- Moderator grants (ROLE-004, ROLE-010). The moderator group is always
-- named: it supplies the command list, and its owner is the responsible
-- server admin (Design 4.3). Without user_id the grant applies to all
-- members of the group; with user_id only to that member.
CREATE TABLE grants (
    id              TEXT NOT NULL PRIMARY KEY,
    user_group_id   TEXT NOT NULL REFERENCES user_groups (id) ON DELETE CASCADE,
    user_id         TEXT REFERENCES users (id) ON DELETE CASCADE,
    server_group_id TEXT REFERENCES server_groups (id) ON DELETE CASCADE,
    server_id       TEXT REFERENCES servers (id) ON DELETE CASCADE,
    granted_by      TEXT NOT NULL REFERENCES users (id),
    created_at      TEXT NOT NULL,
    CHECK ((server_group_id IS NULL) <> (server_id IS NULL))
);
CREATE UNIQUE INDEX grants_unique ON grants (
    user_group_id, coalesce(user_id, ''), coalesce(server_group_id, ''), coalesce(server_id, ''));
CREATE INDEX grants_user ON grants (user_id);

-- Supporter on a server of a moderator (ROLE-006, ROLE-007).
CREATE TABLE supporter_grants (
    id           TEXT NOT NULL PRIMARY KEY,
    supporter_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    moderator_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    server_id    TEXT NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    granted_by   TEXT NOT NULL REFERENCES users (id),
    created_at   TEXT NOT NULL,
    UNIQUE (supporter_id, moderator_id, server_id)
);
CREATE INDEX supporter_grants_moderator ON supporter_grants (moderator_id);

-- Audit log, also the command history of the console (D-029). No foreign
-- keys: entries outlive sessions and soft-deleted records until the
-- retention period ends (Design 4.4). session_id is the session hash.
CREATE TABLE audit_log (
    id          TEXT NOT NULL PRIMARY KEY,
    ts          TEXT NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('command', 'agent', 'admin', 'auth')),
    actor_id    TEXT,
    actor_role  TEXT,
    session_id  TEXT,
    server_id   TEXT,
    target_type TEXT,
    target_id   TEXT,
    action      TEXT NOT NULL,
    payload     TEXT NOT NULL DEFAULT '{}',
    result      TEXT NOT NULL CHECK (result IN ('ok', 'denied', 'error', 'timeout')),
    reason_code TEXT
);
CREATE INDEX audit_log_ts ON audit_log (ts);
CREATE INDEX audit_log_server ON audit_log (server_id, ts);
CREATE INDEX audit_log_target ON audit_log (target_type, target_id, ts);
CREATE INDEX audit_log_actor ON audit_log (actor_id, ts);

-- +goose Down
DROP TABLE audit_log;
DROP TABLE supporter_grants;
DROP TABLE grants;
DROP TABLE admin_scopes;
DROP TABLE server_group_members;
DROP TABLE server_groups;
DROP TABLE servers;
DROP TABLE user_group_members;
DROP TABLE user_groups;
DROP TABLE command_list_entries;
DROP TABLE command_lists;
DROP TABLE invites;
DROP TABLE sessions;
DROP TABLE users;
