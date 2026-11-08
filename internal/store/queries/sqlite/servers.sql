-- Servers and server groups (SRV-001, SRV-003, SRV-005).

-- name: ListServers :many
SELECT id, name, profile_id, protocol, host, port, secret, agent_id, container_name, game_ports, tls_fingerprint
FROM servers WHERE deleted_at IS NULL ORDER BY name COLLATE NOCASE;

-- name: GetServer :one
SELECT id, name, profile_id, protocol, host, port, secret, agent_id, container_name, game_ports, tls_fingerprint
FROM servers WHERE id = ? AND deleted_at IS NULL;

-- name: ListServersByContainer :many
SELECT id FROM servers WHERE agent_id = ? AND container_name = ? AND deleted_at IS NULL;

-- name: InsertServer :exec
INSERT INTO servers (id, name, profile_id, protocol, host, port, secret, game_ports, agent_id, container_name, tls_fingerprint, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateServer :exec
UPDATE servers SET name = ?, profile_id = ?, protocol = ?, host = ?, port = ?, game_ports = ?, agent_id = ?, container_name = ?, tls_fingerprint = ?, updated_at = ?
WHERE id = ?;

-- name: SetServerSecret :exec
UPDATE servers SET secret = ?, updated_at = ? WHERE id = ?;

-- name: SoftDeleteServer :exec
UPDATE servers SET deleted_at = ?, updated_at = ? WHERE id = ?;

-- name: GetGameProfile :one
SELECT id, slug, protocol, default_port, rules FROM game_profiles WHERE id = ?;

-- name: ListServerGroups :many
SELECT id, name FROM server_groups WHERE deleted_at IS NULL ORDER BY name COLLATE NOCASE;

-- name: GetServerGroup :one
SELECT id, name FROM server_groups WHERE id = ? AND deleted_at IS NULL;

-- name: ListServerGroupMemberships :many
SELECT m.server_group_id, m.server_id FROM server_group_members m
JOIN server_groups g ON g.id = m.server_group_id AND g.deleted_at IS NULL
JOIN servers s ON s.id = m.server_id AND s.deleted_at IS NULL
ORDER BY m.server_group_id, m.server_id;

-- name: InsertServerGroup :exec
INSERT INTO server_groups (id, name, created_at, updated_at) VALUES (?, ?, ?, ?);

-- name: RenameServerGroup :exec
UPDATE server_groups SET name = ?, updated_at = ? WHERE id = ?;

-- name: SoftDeleteServerGroup :exec
UPDATE server_groups SET deleted_at = ?, updated_at = ? WHERE id = ?;

-- name: DeleteServerGroupMembers :exec
DELETE FROM server_group_members WHERE server_group_id = ?;

-- name: InsertServerGroupMember :exec
INSERT INTO server_group_members (server_group_id, server_id) VALUES (?, ?);

-- SPDX-License-Identifier: AGPL-3.0-or-later
