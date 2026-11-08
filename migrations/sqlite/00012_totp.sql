-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Second factor (AUTH-008): the TOTP secret sealed with the master key like
-- RCON credentials (SRV-002); totp_pending holds a secret until its first
-- code confirms it; totp_last_step keeps a code from being used twice. The
-- address of the profile is for the recovery mail (NFR-017); only its
-- owner reads it.
ALTER TABLE users ADD COLUMN totp_secret BLOB;
ALTER TABLE users ADD COLUMN totp_pending BLOB;
ALTER TABLE users ADD COLUMN totp_last_step INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT '';

-- A login that passed the password and waits for the code: one use, bound
-- to the user, ends after auth.second_factor_window or 5 wrong codes.
CREATE TABLE login_pending (
    id_hash    TEXT NOT NULL PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    failures   INTEGER NOT NULL DEFAULT 0,
    ip         TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT ''
);
CREATE INDEX login_pending_user ON login_pending (user_id);

-- The recovery link of a user: only the last one requested holds; the
-- token is stored as its SHA-256 only.
CREATE TABLE totp_recovery (
    user_id    TEXT NOT NULL PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE totp_recovery;
DROP TABLE login_pending;
ALTER TABLE users DROP COLUMN email;
ALTER TABLE users DROP COLUMN totp_last_step;
ALTER TABLE users DROP COLUMN totp_pending;
ALTER TABLE users DROP COLUMN totp_secret;
