-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- SMTP for the panel's mail (NFR-017): one row, saved as a whole with its
-- version for If-Match (revision 2.4 candidate 24). The password is sealed
-- with the master key like RCON credentials (SRV-002) and never read out.
CREATE TABLE smtp_settings (
    id         INTEGER NOT NULL PRIMARY KEY CHECK (id = 1),
    host       TEXT NOT NULL DEFAULT '',
    port       INTEGER NOT NULL DEFAULT 587 CHECK (port BETWEEN 1 AND 65535),
    encryption TEXT NOT NULL DEFAULT 'starttls' CHECK (encryption IN ('none', 'starttls', 'ssl')),
    username   TEXT NOT NULL DEFAULT '',
    password   BLOB,
    sender     TEXT NOT NULL DEFAULT '',
    version    INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL DEFAULT ''
);
INSERT INTO smtp_settings (id) VALUES (1);

-- +goose Down
DROP TABLE smtp_settings;
