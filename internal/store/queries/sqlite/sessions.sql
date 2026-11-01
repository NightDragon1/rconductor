-- name: InsertSession :exec
INSERT INTO sessions (id_hash, user_id, created_at, last_seen_at, expires_at, ip, user_agent)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetSessionUser :one
SELECT s.id_hash, s.created_at, s.last_seen_at, s.expires_at,
       u.id AS user_id, u.pseudonym, u.role, u.is_emergency, u.disabled_at, u.deleted_at
FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.id_hash = ?;

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = ? WHERE id_hash = ?;

-- name: DeleteSession :execrows
DELETE FROM sessions WHERE id_hash = ?;

-- SPDX-License-Identifier: AGPL-3.0-or-later
