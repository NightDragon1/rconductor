-- name: InsertMessage :exec
INSERT INTO messages (id, sender_id, sender_pseudonym, audience, text, created_at) VALUES (?, ?, ?, ?, ?, ?);

-- name: InsertMessageRecipient :exec
INSERT INTO message_recipients (message_id, user_id) VALUES (?, ?);

-- name: CountMessageSends :one
SELECT count(*) FROM message_sends WHERE user_id = ? AND sent_at > ?;

-- name: InsertMessageSend :exec
INSERT INTO message_sends (user_id, sent_at) VALUES (?, ?);

-- name: DeleteOldMessageSends :exec
DELETE FROM message_sends WHERE user_id = ? AND sent_at <= ?;

-- name: ListActiveUserRoles :many
SELECT id, role FROM users WHERE deleted_at IS NULL AND disabled_at IS NULL ORDER BY id;

-- name: GetActiveUserRole :one
SELECT role FROM users WHERE id = ? AND deleted_at IS NULL AND disabled_at IS NULL;

-- SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
-- SPDX-License-Identifier: AGPL-3.0-or-later
