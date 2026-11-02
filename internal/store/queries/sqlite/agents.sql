-- Agents and pairing tokens (Design 1.2, 1.7, 4.2).

-- name: InsertAgent :exec
INSERT INTO agents (id, name, public_key, fingerprint, status, version, capabilities, remote, paired_by, created_at, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetAgent :one
SELECT * FROM agents WHERE id = ?;

-- name: GetAgentByFingerprint :one
SELECT * FROM agents WHERE fingerprint = ?;

-- name: ListAgents :many
SELECT * FROM agents ORDER BY status, name, id;

-- name: CountPendingAgents :one
SELECT COUNT(*) FROM agents WHERE status = 'pending';

-- name: DeleteStalePendingAgents :exec
DELETE FROM agents WHERE status = 'pending' AND created_at < ?;

-- name: DeletePendingAgent :execrows
DELETE FROM agents WHERE id = ? AND status = 'pending';

-- name: ActivateAgent :execrows
UPDATE agents SET status = 'active', paired_by = ? WHERE id = ? AND status = 'pending';

-- name: RevokeAgent :execrows
UPDATE agents SET status = 'revoked', revoked_at = ? WHERE id = ? AND status = 'active';

-- name: DetachServersFromAgent :execrows
UPDATE servers SET agent_id = NULL, updated_at = ? WHERE agent_id = ?;

-- name: RepairAgent :execrows
UPDATE agents SET status = 'active', paired_by = ?, revoked_at = NULL, name = ?, version = ?, capabilities = ?, remote = ?, last_seen_at = ?
WHERE id = ? AND status = 'revoked';

-- name: RequestRepairAgent :execrows
UPDATE agents SET status = 'pending', paired_by = NULL, revoked_at = NULL, created_at = ?, last_seen_at = ?
WHERE id = ? AND status = 'revoked';

-- name: TouchAgent :exec
UPDATE agents SET name = ?, version = ?, capabilities = ?, remote = ?, last_seen_at = ? WHERE id = ?;

-- name: InsertAgentToken :exec
INSERT INTO agent_tokens (id, token_hash, created_by, created_at, expires_at) VALUES (?, ?, ?, ?, ?);

-- name: GetAgentTokenByHash :one
SELECT * FROM agent_tokens WHERE token_hash = ?;

-- name: UseAgentToken :execrows
UPDATE agent_tokens SET used_at = ?, agent_id = ? WHERE id = ? AND used_at IS NULL AND expires_at > ?;

-- SPDX-License-Identifier: AGPL-3.0-or-later
