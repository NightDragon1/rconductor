-- Command history: a view on the append-only audit log (D-029). Each
-- command has a cmd.sent entry (ID = request ID) and later a cmd.result
-- entry that refers to it; a refusal is one cmd.denied entry.

-- name: ListCommandHistory :many
SELECT a.id, a.ts, a.actor_id, a.actor_role, a.action, a.payload, a.result, a.reason_code,
       coalesce(u.pseudonym, '') AS pseudonym,
       r.result AS final_result, r.reason_code AS final_reason, r.payload AS result_payload
FROM audit_log a
LEFT JOIN users u ON u.id = a.actor_id
LEFT JOIN audit_log r ON r.kind = 'command' AND r.action = 'cmd.result' AND r.target_type = 'command' AND r.target_id = a.id
WHERE a.kind = 'command' AND a.action IN ('cmd.sent', 'cmd.denied') AND a.server_id = sqlc.arg(server_id)
  AND (sqlc.arg(actor_id) = '' OR a.actor_id = sqlc.arg(actor_id))
  AND (sqlc.arg(cursor_ts) = '' OR a.ts < sqlc.arg(cursor_ts) OR (a.ts = sqlc.arg(cursor_ts) AND a.id < sqlc.arg(cursor_id)))
ORDER BY a.ts DESC, a.id DESC
LIMIT sqlc.arg(lim);

-- SPDX-License-Identifier: AGPL-3.0-or-later
