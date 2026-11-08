-- Command history: a view on the append-only audit log (D-029). Each
-- command has a cmd.sent entry (ID = request ID) and later a cmd.result
-- entry that refers to it; a refusal is one cmd.denied entry.

-- name: ListCommandHistory :many
-- The author counts with the role at the time of the command; for entries
-- of 1.0 without it the role stored with the user, also a deleted one
-- (soft delete, D-028); only without any user record as an RCON admin
-- (ROLE-009, D-067).
SELECT a.id, a.ts, a.actor_id, a.actor_role, a.action, a.payload, a.result, a.reason_code,
       coalesce(u.pseudonym, '') AS pseudonym,
       CAST(CASE WHEN coalesce(a.actor_role, '') != '' THEN a.actor_role
            WHEN u.id IS NOT NULL THEN u.role ELSE 'rcon_admin' END AS TEXT) AS author_role,
       r.result AS final_result, r.reason_code AS final_reason, r.payload AS result_payload
FROM audit_log a
LEFT JOIN users u ON u.id = a.actor_id
LEFT JOIN audit_log r ON r.kind = 'command' AND r.action = 'cmd.result' AND r.target_type = 'command' AND r.target_id = a.id
WHERE a.kind = 'command' AND a.action IN ('cmd.sent', 'cmd.denied') AND a.server_id = sqlc.arg(server_id)
  AND (sqlc.arg(actor_id) = '' OR a.actor_id = sqlc.arg(actor_id))
  -- Ranks of perm.AuthorRank: rcon_admin 0, server_admin 1, moderator and
  -- supporter 2, anything else 0. A viewer sees authors from his rank on.
  AND (CASE (CASE WHEN coalesce(a.actor_role, '') != '' THEN a.actor_role
                  WHEN u.id IS NOT NULL THEN u.role ELSE 'rcon_admin' END)
       WHEN 'server_admin' THEN 1 WHEN 'moderator' THEN 2 WHEN 'supporter' THEN 2 ELSE 0 END) >= CAST(sqlc.arg(min_rank) AS INTEGER)
  AND (sqlc.arg(cursor_ts) = '' OR a.ts < sqlc.arg(cursor_ts) OR (a.ts = sqlc.arg(cursor_ts) AND a.id < sqlc.arg(cursor_id)))
ORDER BY a.ts DESC, a.id DESC
LIMIT sqlc.arg(lim);

-- SPDX-License-Identifier: AGPL-3.0-or-later
