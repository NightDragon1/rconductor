-- Reading the audit log (AUD-004), newest first, cursor = (ts, id).

-- name: ListAuditAll :many
SELECT id, ts, kind, actor_id, actor_role, session_id, server_id, target_type, target_id,
       action, payload, result, reason_code
FROM audit_log
WHERE (sqlc.arg(cursor_ts) = '' OR ts < sqlc.arg(cursor_ts) OR (ts = sqlc.arg(cursor_ts) AND id < sqlc.arg(cursor_id)))
  AND (sqlc.arg(kind) = '' OR kind = sqlc.arg(kind))
  AND (sqlc.arg(server_id) = '' OR server_id = sqlc.arg(server_id))
ORDER BY ts DESC, id DESC
LIMIT sqlc.arg(lim);

-- name: ListAuditScoped :many
SELECT id, ts, kind, actor_id, actor_role, session_id, server_id, target_type, target_id,
       action, payload, result, reason_code
FROM audit_log
WHERE (sqlc.arg(cursor_ts) = '' OR ts < sqlc.arg(cursor_ts) OR (ts = sqlc.arg(cursor_ts) AND id < sqlc.arg(cursor_id)))
  AND (sqlc.arg(kind) = '' OR kind = sqlc.arg(kind))
  AND (sqlc.arg(server_id) = '' OR server_id = sqlc.arg(server_id))
  AND (server_id IN (SELECT value FROM json_each(sqlc.arg(servers)))
       OR (target_type = 'user' AND target_id IN (SELECT value FROM json_each(sqlc.arg(users))))
       OR actor_id = sqlc.arg(self))
ORDER BY ts DESC, id DESC
LIMIT sqlc.arg(lim);

-- name: ListGlobalSettings :many
SELECT key, value FROM settings WHERE scope_type = 'global' ORDER BY key;

-- name: SetGlobalSetting :exec
UPDATE settings SET value = ?, updated_at = ? WHERE key = ? AND scope_type = 'global' AND scope_id = '';

-- name: ListSupporterIDs :many
SELECT id FROM users WHERE role = 'supporter' AND deleted_at IS NULL;

-- SPDX-License-Identifier: AGPL-3.0-or-later
