-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Favorite servers of a user for the console (CMD-001, revision 2.4
-- candidate 6); they follow the user on every device.
CREATE TABLE user_favorites (
    user_id   TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    server_id TEXT NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    position  INTEGER NOT NULL,
    PRIMARY KEY (user_id, server_id)
);

-- +goose Down
DROP TABLE user_favorites;
