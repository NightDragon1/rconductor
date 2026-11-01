-- Inputs of the permission module (Design 3.2, 4.3). Soft-deleted servers,
-- groups and lists and inactive owners never grant anything.

-- name: GetRightsUser :one
SELECT id, role, supporters_allowed, disabled_at, deleted_at FROM users WHERE id = ?;

-- name: ListActiveServerIDs :many
SELECT id FROM servers WHERE deleted_at IS NULL ORDER BY id;

-- name: ListAdminScopeServerIDs :many
SELECT s.id FROM admin_scopes a
JOIN servers s ON s.id = a.server_id AND s.deleted_at IS NULL
WHERE a.user_id = sqlc.arg(user_id)
UNION
SELECT s.id FROM admin_scopes a
JOIN server_groups g ON g.id = a.server_group_id AND g.deleted_at IS NULL
JOIN server_group_members m ON m.server_group_id = g.id
JOIN servers s ON s.id = m.server_id AND s.deleted_at IS NULL
WHERE a.user_id = sqlc.arg(user_id);

-- Servers a moderator reaches through grants of moderator groups he is a
-- member of: grants to the whole group, or to him by name. Each row names
-- the group's command list and owner, the responsible server admin.
-- name: ListModeratorGrants :many
SELECT DISTINCT s.id AS server_id, g.id AS group_id, g.command_list_id, g.owner_id
FROM grants gr
JOIN user_groups g ON g.id = gr.user_group_id AND g.kind = 'moderator' AND g.deleted_at IS NULL
JOIN user_group_members m ON m.user_group_id = g.id AND m.user_id = sqlc.arg(user_id)
JOIN servers s ON s.deleted_at IS NULL AND (
    s.id = gr.server_id OR s.id IN (
        SELECT sgm.server_id FROM server_group_members sgm
        JOIN server_groups sg ON sg.id = sgm.server_group_id AND sg.deleted_at IS NULL
        WHERE sgm.server_group_id = gr.server_group_id))
WHERE gr.user_id IS NULL OR gr.user_id = sqlc.arg(user_id);

-- name: ListCommandListEntries :many
SELECT e.path, e.passable FROM command_list_entries e
JOIN command_lists l ON l.id = e.command_list_id AND l.deleted_at IS NULL
WHERE e.command_list_id = ?
ORDER BY e.path;

-- name: ListSupporterGrants :many
SELECT moderator_id, server_id FROM supporter_grants WHERE supporter_id = ?;

-- SPDX-License-Identifier: AGPL-3.0-or-later
