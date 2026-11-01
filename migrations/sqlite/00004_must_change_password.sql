-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Forced password change (AUTH-010): set with the initial password and by a
-- password reset, cleared by the user's own change.
ALTER TABLE users ADD COLUMN must_change_password INTEGER NOT NULL DEFAULT 0
    CHECK (must_change_password IN (0, 1));

-- +goose Down
ALTER TABLE users DROP COLUMN must_change_password;
