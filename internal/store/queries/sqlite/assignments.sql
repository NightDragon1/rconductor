-- Assignments of one user (Design 2.2 /users/{id}/assignments).

-- name: ListUserScopes :many
SELECT id, server_group_id, server_id FROM admin_scopes WHERE user_id = ? ORDER BY id;

-- name: ListUserMemberships :many
SELECT g.id, g.kind, g.owner_id FROM user_group_members m
JOIN user_groups g ON g.id = m.user_group_id AND g.deleted_at IS NULL
WHERE m.user_id = ? ORDER BY g.id;

-- name: ListUserDirectGrants :many
SELECT gr.id, gr.user_group_id, gr.server_group_id, gr.server_id, g.owner_id FROM grants gr
JOIN user_groups g ON g.id = gr.user_group_id
WHERE gr.user_id = ? ORDER BY gr.id;

-- name: ListUserSupporterGrants :many
SELECT id, moderator_id, server_id FROM supporter_grants WHERE supporter_id = ? ORDER BY id;

-- name: GetActiveGroup :one
SELECT id, kind, owner_id FROM user_groups WHERE id = ? AND deleted_at IS NULL;

-- name: ServerExists :one
SELECT count(*) FROM servers WHERE id = ? AND deleted_at IS NULL;

-- name: ServerGroupExists :one
SELECT count(*) FROM server_groups WHERE id = ? AND deleted_at IS NULL;

-- name: ListServerGroupServerIDs :many
SELECT s.id FROM server_group_members m
JOIN servers s ON s.id = m.server_id AND s.deleted_at IS NULL
WHERE m.server_group_id = ?;

-- name: InsertAdminScope :exec
INSERT INTO admin_scopes (id, user_id, server_group_id, server_id, assigned_by, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: DeleteAdminScope :exec
DELETE FROM admin_scopes WHERE id = ?;

-- name: InsertMembership :exec
INSERT INTO user_group_members (user_group_id, user_id, added_by, created_at) VALUES (?, ?, ?, ?);

-- name: DeleteMembership :exec
DELETE FROM user_group_members WHERE user_group_id = ? AND user_id = ?;

-- name: InsertGrant :exec
INSERT INTO grants (id, user_group_id, user_id, server_group_id, server_id, granted_by, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: DeleteGrant :exec
DELETE FROM grants WHERE id = ?;

-- name: InsertSupporterGrant :exec
INSERT INTO supporter_grants (id, supporter_id, moderator_id, server_id, granted_by, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: DeleteSupporterGrant :exec
DELETE FROM supporter_grants WHERE id = ?;

-- SPDX-License-Identifier: AGPL-3.0-or-later
