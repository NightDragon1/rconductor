-- Invitations (UI-008, AUTH-001); tokens only as SHA-256 hash.

-- name: InsertInvite :exec
INSERT INTO invites (id, token_hash, created_by, role, template_user_id, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListInvites :many
SELECT id, created_by, role, template_user_id, created_at, expires_at, used_at, used_by
FROM invites ORDER BY created_at DESC;

-- name: GetInvite :one
SELECT id, created_by, role, template_user_id, created_at, expires_at, used_at, used_by
FROM invites WHERE id = ?;

-- name: GetInviteByToken :one
SELECT id, created_by, role, template_user_id, expires_at, used_at FROM invites WHERE token_hash = ?;

-- name: UseInvite :execrows
UPDATE invites SET used_at = ?, used_by = ? WHERE id = ? AND used_at IS NULL;

-- name: DeleteInvite :exec
DELETE FROM invites WHERE id = ?;

-- SPDX-License-Identifier: AGPL-3.0-or-later
