-- Command templates per server (CMD-008, D-068).

-- name: ListTemplates :many
SELECT id, server_id, name, template, version FROM command_templates
WHERE server_id = ? AND deleted_at IS NULL ORDER BY name COLLATE NOCASE, id;

-- name: GetTemplate :one
SELECT id, server_id, name, template, version FROM command_templates
WHERE id = ? AND server_id = ? AND deleted_at IS NULL;

-- name: InsertTemplate :execrows
-- The count and the insert are one statement, so that parallel requests
-- cannot pass commands.templates_per_server.
INSERT INTO command_templates (id, server_id, name, template, created_by, created_at, updated_at)
SELECT sqlc.arg(id), sqlc.arg(server_id), sqlc.arg(name), sqlc.arg(template), sqlc.arg(created_by), sqlc.arg(created_at), sqlc.arg(updated_at)
WHERE (SELECT count(*) FROM command_templates WHERE server_id = sqlc.arg(server_id) AND deleted_at IS NULL) < CAST(sqlc.arg(max_templates) AS INTEGER);

-- name: UpdateTemplate :execrows
UPDATE command_templates SET name = ?, template = ?, version = version + 1, updated_at = ?
WHERE id = ? AND server_id = ? AND version = ? AND deleted_at IS NULL;

-- name: SoftDeleteTemplate :execrows
UPDATE command_templates SET deleted_at = ?, updated_at = ?
WHERE id = ? AND server_id = ? AND deleted_at IS NULL;

-- name: SoftDeleteServerTemplates :exec
UPDATE command_templates SET deleted_at = ?, updated_at = ?
WHERE server_id = ? AND deleted_at IS NULL;

-- SPDX-License-Identifier: AGPL-3.0-or-later
