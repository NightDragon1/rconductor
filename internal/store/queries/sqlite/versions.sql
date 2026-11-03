-- Optimistic locking (revision 2.4 candidate 24): a write raises the
-- version it read in one statement; no row means it was stale.

-- name: BumpUserGroupVersion :execrows
UPDATE user_groups SET version = version + 1 WHERE id = ? AND version = ? AND deleted_at IS NULL;

-- name: BumpCommandListVersion :execrows
UPDATE command_lists SET version = version + 1 WHERE id = ? AND version = ? AND deleted_at IS NULL;

-- name: BumpServerVersion :execrows
UPDATE servers SET version = version + 1 WHERE id = ? AND version = ? AND deleted_at IS NULL;

-- name: BumpServerGroupVersion :execrows
UPDATE server_groups SET version = version + 1 WHERE id = ? AND version = ? AND deleted_at IS NULL;

-- name: BumpSettingsVersion :execrows
UPDATE settings_version SET version = version + 1 WHERE id = 1 AND version = ?;

-- name: ListUserGroupVersions :many
SELECT id, version FROM user_groups WHERE deleted_at IS NULL;

-- name: ListCommandListVersions :many
SELECT id, version FROM command_lists WHERE deleted_at IS NULL;

-- name: ListServerVersions :many
SELECT id, version FROM servers WHERE deleted_at IS NULL;

-- name: ListServerGroupVersions :many
SELECT id, version FROM server_groups WHERE deleted_at IS NULL;

-- name: GetSettingsVersion :one
SELECT version FROM settings_version WHERE id = 1;

-- SPDX-License-Identifier: AGPL-3.0-or-later
