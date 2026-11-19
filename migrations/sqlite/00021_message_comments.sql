-- SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Comments on messages (MSG-003, D-085, D-086). comment_sends counts the
-- comments of each author for messages.comments_per_hour on its own, so
-- deleting a comment does not lower the count.
CREATE TABLE message_comments (
    id               TEXT NOT NULL PRIMARY KEY,
    message_id       TEXT NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    author_id        TEXT REFERENCES users (id) ON DELETE SET NULL,
    author_pseudonym TEXT NOT NULL,
    text             TEXT NOT NULL,
    created_at       TEXT NOT NULL
);
CREATE INDEX message_comments_message ON message_comments (message_id, created_at, id);
CREATE TABLE comment_sends (
    user_id TEXT NOT NULL,
    sent_at TEXT NOT NULL
);
CREATE INDEX comment_sends_user ON comment_sends (user_id, sent_at);

-- +goose Down
DROP TABLE comment_sends;
DROP TABLE message_comments;
