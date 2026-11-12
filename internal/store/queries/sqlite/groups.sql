-- User groups and moderator groups (AUTH-002, ROLE-005).

-- name: ListGroups :many
SELECT id, name, kind, owner_id, command_list_id FROM user_groups
WHERE deleted_at IS NULL ORDER BY name COLLATE NOCASE;

-- name: GetGroup :one
SELECT id, name, kind, owner_id, command_list_id FROM user_groups WHERE id = ? AND deleted_at IS NULL;

-- name: InsertGroup :exec
INSERT INTO user_groups (id, name, kind, owner_id, command_list_id, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: UpdateGroup :exec
UPDATE user_groups SET name = ?, command_list_id = ?, updated_at = ? WHERE id = ?;

-- name: SoftDeleteGroup :exec
UPDATE user_groups SET deleted_at = ?, updated_at = ? WHERE id = ?;

-- name: ListGroupMembers :many
SELECT user_id FROM user_group_members WHERE user_group_id = ? ORDER BY user_id;

-- name: ListGroupGrants :many
SELECT id, server_group_id, server_id FROM grants
WHERE user_group_id = ? AND user_id IS NULL ORDER BY id;

-- name: DeleteUserGrantsInGroup :exec
DELETE FROM grants WHERE user_group_id = ? AND user_id = ?;

-- name: GetCommandListOwner :one
SELECT owner_id FROM command_lists WHERE id = ? AND deleted_at IS NULL;

-- name: DeleteGroupGrants :many
-- Grants end with their user group (ROLE-004); see DeleteServerGrants.
DELETE FROM grants WHERE user_group_id = ? RETURNING id, user_id, server_group_id, server_id;

-- SPDX-License-Identifier: AGPL-3.0-or-later
