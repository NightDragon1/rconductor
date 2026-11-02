-- Command history: a view on the audit log (D-029, Design 4.1).

-- name: SetAuditResult :exec
UPDATE audit_log SET result = ?, reason_code = ?, payload = ? WHERE id = ?;

-- name: ListCommandHistory :many
SELECT a.id, a.ts, a.actor_id, a.actor_role, a.payload, a.result, a.reason_code,
       coalesce(u.pseudonym, '') AS pseudonym
FROM audit_log a LEFT JOIN users u ON u.id = a.actor_id
WHERE a.kind = 'command' AND a.server_id = sqlc.arg(server_id)
  AND (sqlc.arg(actor_id) = '' OR a.actor_id = sqlc.arg(actor_id))
  AND (sqlc.arg(cursor_ts) = '' OR a.ts < sqlc.arg(cursor_ts) OR (a.ts = sqlc.arg(cursor_ts) AND a.id < sqlc.arg(cursor_id)))
ORDER BY a.ts DESC, a.id DESC
LIMIT sqlc.arg(lim);

-- SPDX-License-Identifier: AGPL-3.0-or-later
