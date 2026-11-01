-- Command lists with token paths (ROLE-005, ROLE-007).

-- name: ListCommandLists :many
SELECT id, name, owner_id FROM command_lists WHERE deleted_at IS NULL ORDER BY name COLLATE NOCASE;

-- name: GetCommandList :one
SELECT id, name, owner_id FROM command_lists WHERE id = ? AND deleted_at IS NULL;

-- name: InsertCommandList :exec
INSERT INTO command_lists (id, name, owner_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?);

-- name: RenameCommandList :exec
UPDATE command_lists SET name = ?, updated_at = ? WHERE id = ?;

-- name: SoftDeleteCommandList :exec
UPDATE command_lists SET deleted_at = ?, updated_at = ? WHERE id = ?;

-- name: DeleteCommandListEntries :exec
DELETE FROM command_list_entries WHERE command_list_id = ?;

-- name: InsertCommandListEntry :exec
INSERT INTO command_list_entries (id, command_list_id, path, passable) VALUES (?, ?, ?, ?);

-- SPDX-License-Identifier: AGPL-3.0-or-later
