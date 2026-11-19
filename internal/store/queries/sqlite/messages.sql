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

-- name: ListMessagesView :many
SELECT m.id, m.sender_id, m.sender_pseudonym, m.audience, m.text, m.created_at,
  (SELECT count(*) FROM message_recipients r WHERE r.message_id = m.id) AS recipients,
  (SELECT count(*) FROM message_recipients r WHERE r.message_id = m.id AND r.user_id = ?1) AS mine,
  (SELECT count(*) FROM message_recipients r WHERE r.message_id = m.id AND r.user_id = ?1 AND r.read_at IS NULL) AS unread
FROM messages m
WHERE (?2 = 1 OR m.sender_id = ?1 OR EXISTS (SELECT 1 FROM message_recipients r WHERE r.message_id = m.id AND r.user_id = ?1))
  AND (?3 = '' OR m.created_at < ?3 OR (m.created_at = ?3 AND m.id < ?4))
  AND (?5 = '' OR m.id = ?5)
ORDER BY m.created_at DESC, m.id DESC
LIMIT ?6;

-- name: CountUnreadMessages :one
SELECT count(*) FROM message_recipients WHERE user_id = ? AND read_at IS NULL;

-- name: MarkMessageRead :execrows
UPDATE message_recipients SET read_at = ? WHERE message_id = ? AND user_id = ? AND read_at IS NULL;

-- name: DeleteMessage :execrows
DELETE FROM messages WHERE id = ?;

-- name: IsMessageRecipient :one
SELECT count(*) FROM message_recipients WHERE message_id = ? AND user_id = ?;

-- SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
-- SPDX-License-Identifier: AGPL-3.0-or-later
