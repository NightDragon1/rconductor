-- SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Messages in the panel (MSG-001 to MSG-003, D-085, D-086). The
-- recipients are fixed when a message is sent. message_sends counts the
-- sends of each sender for messages.per_hour on its own, so deleting a
-- message does not lower the count.
CREATE TABLE messages (
    id               TEXT NOT NULL PRIMARY KEY,
    sender_id        TEXT REFERENCES users (id) ON DELETE SET NULL,
    sender_pseudonym TEXT NOT NULL,
    audience         TEXT NOT NULL,
    text             TEXT NOT NULL,
    created_at       TEXT NOT NULL
);
CREATE INDEX messages_created ON messages (created_at);
CREATE INDEX messages_sender ON messages (sender_id);
CREATE TABLE message_recipients (
    message_id TEXT NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    read_at    TEXT,
    PRIMARY KEY (message_id, user_id)
);
CREATE INDEX message_recipients_user ON message_recipients (user_id);
CREATE TABLE message_sends (
    user_id TEXT NOT NULL,
    sent_at TEXT NOT NULL
);
CREATE INDEX message_sends_user ON message_sends (user_id, sent_at);

-- +goose Down
DROP TABLE message_sends;
DROP TABLE message_recipients;
DROP TABLE messages;
