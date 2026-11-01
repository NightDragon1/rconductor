-- name: InsertAudit :exec
INSERT INTO audit_log (
    id, ts, kind, actor_id, actor_role, session_id, server_id,
    target_type, target_id, action, payload, result, reason_code
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- SPDX-License-Identifier: AGPL-3.0-or-later
