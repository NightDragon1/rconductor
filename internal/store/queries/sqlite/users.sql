-- name: GetEmergencyAdmin :one
SELECT id, pseudonym FROM users WHERE is_emergency = 1;

-- name: InsertUser :exec
INSERT INTO users (
    id, pseudonym, role, password_hash, is_emergency, created_by, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetLoginUser :one
SELECT id, pseudonym, role, password_hash, is_emergency, disabled_at
FROM users
WHERE pseudonym = ? COLLATE NOCASE AND deleted_at IS NULL;

-- name: SetLastLogin :exec
UPDATE users SET last_login_at = ? WHERE id = ?;

-- SPDX-License-Identifier: AGPL-3.0-or-later
