-- Daily clean-up (Design 4.4, AUD-003).

-- name: PurgeAudit :execrows
DELETE FROM audit_log WHERE ts < ?;

-- name: PurgeSessions :execrows
DELETE FROM sessions WHERE expires_at < sqlc.arg(now) OR last_seen_at < sqlc.arg(idle_before);

-- name: PurgeInvites :execrows
DELETE FROM invites WHERE expires_at < ?;

-- Soft-deleted records that no audit entry refers to any more.
-- name: ListPurgeableUsers :many
SELECT id FROM users u WHERE u.deleted_at IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM audit_log a WHERE a.actor_id = u.id OR (a.target_type = 'user' AND a.target_id = u.id));

-- name: ListPurgeableGroups :many
SELECT id FROM user_groups g WHERE g.deleted_at IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM audit_log a WHERE a.target_type = 'user_group' AND a.target_id = g.id);

-- name: ListPurgeableCommandLists :many
SELECT id FROM command_lists l WHERE l.deleted_at IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM audit_log a WHERE a.target_type = 'command_list' AND a.target_id = l.id);

-- name: ListPurgeableServers :many
SELECT id FROM servers s WHERE s.deleted_at IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM audit_log a WHERE a.server_id = s.id);

-- name: ListPurgeableServerGroups :many
SELECT id FROM server_groups g WHERE g.deleted_at IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM audit_log a WHERE a.target_type = 'server_group' AND a.target_id = g.id);

-- name: DeleteUserRecord :exec
DELETE FROM users WHERE id = ? AND deleted_at IS NOT NULL;

-- name: DeleteGroupRecord :exec
DELETE FROM user_groups WHERE id = ? AND deleted_at IS NOT NULL;

-- name: DeleteCommandListRecord :exec
DELETE FROM command_lists WHERE id = ? AND deleted_at IS NOT NULL;

-- name: DeleteServerRecord :exec
DELETE FROM servers WHERE id = ? AND deleted_at IS NOT NULL;

-- name: DeleteServerGroupRecord :exec
DELETE FROM server_groups WHERE id = ? AND deleted_at IS NOT NULL;

-- SPDX-License-Identifier: AGPL-3.0-or-later
