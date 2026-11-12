-- Servers and server groups (SRV-001, SRV-003, SRV-005).

-- name: ListServers :many
-- profile_id is the top profile of the server (SRV-008); the whole list
-- comes from ListServerProfiles.
SELECT s.id, s.name, sp.profile_id, s.protocol, s.host, s.port, s.secret, s.agent_id, s.container_name, s.game_ports, s.tls_fingerprint, s.tls_not_after
FROM servers s LEFT JOIN server_profiles sp ON sp.server_id = s.id AND sp.position = 0
WHERE s.deleted_at IS NULL ORDER BY s.name COLLATE NOCASE;

-- name: GetServer :one
SELECT s.id, s.name, sp.profile_id, s.protocol, s.host, s.port, s.secret, s.agent_id, s.container_name, s.game_ports, s.tls_fingerprint, s.tls_not_after
FROM servers s LEFT JOIN server_profiles sp ON sp.server_id = s.id AND sp.position = 0
WHERE s.id = ? AND s.deleted_at IS NULL;

-- name: ListServersByContainer :many
SELECT id FROM servers WHERE agent_id = ? AND container_name = ? AND deleted_at IS NULL;

-- name: InsertServer :exec
INSERT INTO servers (id, name, protocol, host, port, secret, game_ports, agent_id, container_name, tls_fingerprint, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateServer :exec
UPDATE servers SET name = ?, protocol = ?, host = ?, port = ?, game_ports = ?, agent_id = ?, container_name = ?, tls_fingerprint = ?, tls_not_after = ?, updated_at = ?
WHERE id = ?;

-- name: SetServerNotAfter :execrows
-- The end of validity of the certificate the pin names, read when the pin
-- was saved (D-073).
UPDATE servers SET tls_not_after = ? WHERE id = ? AND tls_fingerprint = ? AND tls_fingerprint <> '' AND deleted_at IS NULL;

-- name: FillServerNotAfter :execrows
-- Fills in the end of validity at the first connection with the pin (D-073).
UPDATE servers SET tls_not_after = ? WHERE id = ? AND tls_fingerprint = ? AND tls_fingerprint <> '' AND tls_not_after IS NULL AND deleted_at IS NULL;

-- name: ListServerProfiles :many
SELECT server_id, profile_id, position FROM server_profiles ORDER BY server_id, position;

-- name: GetServerProfiles :many
SELECT profile_id FROM server_profiles WHERE server_id = ? ORDER BY position;

-- name: ClearServerProfiles :exec
DELETE FROM server_profiles WHERE server_id = ?;

-- name: AddServerProfile :exec
INSERT INTO server_profiles (server_id, profile_id, position) VALUES (?, ?, ?);

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

-- Assignments end with the server or server group they name (ROLE-003,
-- ROLE-004, ROLE-007): the soft delete keeps the row, so ON DELETE CASCADE
-- never fires. The rows come back for the audit.

-- name: DeleteServerScopes :many
DELETE FROM admin_scopes WHERE server_id = ? RETURNING id, user_id;

-- name: DeleteServerGrants :many
DELETE FROM grants WHERE server_id = ? RETURNING id, user_group_id, user_id;

-- name: DeleteServerSupporterGrants :many
DELETE FROM supporter_grants WHERE server_id = ? RETURNING id, supporter_id, moderator_id;

-- name: DeleteServerGroupScopes :many
DELETE FROM admin_scopes WHERE server_group_id = ? RETURNING id, user_id;

-- name: DeleteServerGroupGrants :many
DELETE FROM grants WHERE server_group_id = ? RETURNING id, user_group_id, user_id;

-- SPDX-License-Identifier: AGPL-3.0-or-later
