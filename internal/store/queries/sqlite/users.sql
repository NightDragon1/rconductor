-- name: GetEmergencyAdmin :one
SELECT id, pseudonym FROM users WHERE is_emergency = 1;

-- name: InsertUser :exec
INSERT INTO users (
    id, pseudonym, role, password_hash, is_emergency, must_change_password, created_by, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetLoginUser :one
SELECT id, pseudonym, role, password_hash, is_emergency, disabled_at, must_change_password
FROM users
WHERE pseudonym = ? COLLATE NOCASE AND deleted_at IS NULL;

-- name: SetLastLogin :exec
UPDATE users SET last_login_at = ? WHERE id = ?;

-- name: ListUsers :many
SELECT id, pseudonym, role, real_name, contacts, CAST(avatar IS NOT NULL AS INTEGER) AS has_avatar,
       disabled_at, is_emergency, supporters_allowed, created_at, last_login_at
FROM users WHERE deleted_at IS NULL
ORDER BY pseudonym COLLATE NOCASE;

-- name: GetUser :one
SELECT id, pseudonym, role, real_name, contacts, CAST(avatar IS NOT NULL AS INTEGER) AS has_avatar,
       disabled_at, is_emergency, supporters_allowed, created_at, last_login_at
FROM users WHERE id = ? AND deleted_at IS NULL;

-- name: SetUserRole :exec
UPDATE users SET role = ?, updated_at = ? WHERE id = ?;

-- name: SetUserDisabled :exec
UPDATE users SET disabled_at = ?, updated_at = ? WHERE id = ?;

-- name: SetSupportersAllowed :exec
UPDATE users SET supporters_allowed = ?, updated_at = ? WHERE id = ?;

-- name: ListSupportersOfModerator :many
SELECT DISTINCT supporter_id FROM supporter_grants WHERE moderator_id = ?;

-- name: UpdateProfile :exec
UPDATE users SET pseudonym = ?, real_name = ?, contacts = ?, updated_at = ? WHERE id = ?;

-- name: GetPasswordHash :one
SELECT password_hash FROM users WHERE id = ? AND deleted_at IS NULL;

-- The user's own change clears a forced change (AUTH-010).
-- name: SetPasswordHash :exec
UPDATE users SET password_hash = ?, must_change_password = 0, updated_at = ? WHERE id = ?;

-- A reset sets a temporary password that must be changed (AUTH-010); a
-- lock stays.
-- name: ResetPassword :exec
UPDATE users SET password_hash = ?, must_change_password = 1, updated_at = ? WHERE id = ?;

-- Recovery of the emergency admin also lifts a lock (AUTH-011).
-- name: ResetEmergencyAdmin :exec
UPDATE users SET password_hash = ?, must_change_password = 1, disabled_at = NULL, updated_at = ?
WHERE id = ? AND is_emergency = 1;

-- name: SetAvatar :exec
UPDATE users SET avatar = ?, avatar_type = ?, updated_at = ? WHERE id = ?;

-- name: GetAvatar :one
SELECT avatar, avatar_type FROM users WHERE id = ? AND deleted_at IS NULL AND avatar IS NOT NULL;

-- name: SoftDeleteUser :exec
UPDATE users SET deleted_at = ?, updated_at = ? WHERE id = ?;

-- Assignments belong to a role; a role change or deletion removes them.
-- name: DeleteUserScopes :exec
DELETE FROM admin_scopes WHERE user_id = ?;

-- name: DeleteUserMemberships :exec
DELETE FROM user_group_members WHERE user_id = ?;

-- name: DeleteUserGrants :exec
DELETE FROM grants WHERE user_id = ?;

-- name: DeleteUserSupporterGrants :exec
DELETE FROM supporter_grants WHERE supporter_id = sqlc.arg(user_id) OR moderator_id = sqlc.arg(user_id);

-- SPDX-License-Identifier: AGPL-3.0-or-later
