-- name: GetSecondFactor :one
SELECT id, pseudonym, role, password_hash, totp_secret, totp_pending, totp_last_step, email, disabled_at
FROM users WHERE id = ? AND deleted_at IS NULL;

-- name: SetTOTPPending :execrows
UPDATE users SET totp_pending = ?, updated_at = ? WHERE id = ? AND totp_secret IS NULL;

-- name: ActivateTOTP :execrows
UPDATE users SET totp_secret = totp_pending, totp_pending = NULL, totp_last_step = ?, updated_at = ?
WHERE id = ? AND totp_pending IS NOT NULL AND totp_secret IS NULL;

-- name: ClearTOTP :execrows
UPDATE users SET totp_secret = NULL, totp_pending = NULL, totp_last_step = 0, updated_at = ?
WHERE id = ? AND (totp_secret IS NOT NULL OR totp_pending IS NOT NULL);

-- name: UseTOTPStep :execrows
UPDATE users SET totp_last_step = sqlc.arg(step) WHERE id = sqlc.arg(id) AND totp_last_step < sqlc.arg(step);

-- name: SetEmail :exec
UPDATE users SET email = ?, updated_at = ? WHERE id = ?;

-- name: InsertLoginPending :exec
INSERT INTO login_pending (id_hash, user_id, created_at, expires_at, ip, user_agent) VALUES (?, ?, ?, ?, ?, ?);

-- name: GetLoginPending :one
SELECT p.id_hash, p.user_id, p.expires_at, p.failures, p.ip, p.user_agent,
       u.pseudonym, u.role, u.password_hash, u.is_emergency, u.disabled_at, u.must_change_password,
       u.totp_secret, u.totp_last_step
FROM login_pending p JOIN users u ON u.id = p.user_id
WHERE p.id_hash = ? AND u.deleted_at IS NULL;

-- name: AddPendingFailure :one
UPDATE login_pending SET failures = failures + 1 WHERE id_hash = ? RETURNING failures;

-- name: DeleteLoginPending :execrows
DELETE FROM login_pending WHERE id_hash = ?;

-- name: DeleteExpiredLoginPending :exec
DELETE FROM login_pending WHERE expires_at < ?;

-- name: PutRecovery :exec
INSERT INTO totp_recovery (user_id, token_hash, created_at, expires_at) VALUES (?, ?, ?, ?)
ON CONFLICT (user_id) DO UPDATE SET token_hash = excluded.token_hash, created_at = excluded.created_at,
    expires_at = excluded.expires_at;

-- name: GetRecovery :one
SELECT r.user_id, r.expires_at, u.pseudonym, u.role, u.password_hash, u.disabled_at
FROM totp_recovery r JOIN users u ON u.id = r.user_id
WHERE r.token_hash = ? AND u.deleted_at IS NULL;

-- name: TakeRecovery :execrows
DELETE FROM totp_recovery WHERE token_hash = ? AND user_id = ?;

-- name: GetRecoveryTarget :one
SELECT id, email, totp_secret IS NOT NULL AS totp_enabled, disabled_at
FROM users WHERE pseudonym = ? COLLATE NOCASE AND deleted_at IS NULL;

-- SPDX-License-Identifier: AGPL-3.0-or-later
