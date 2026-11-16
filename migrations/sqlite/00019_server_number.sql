-- SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Consecutive server number (UI-018). Existing servers, deleted ones
-- included, are numbered by creation time. New numbers come from a
-- counter that never goes down, so a number freed by a deleted server is
-- never given again.
ALTER TABLE servers ADD COLUMN number INTEGER;
UPDATE servers SET number = (
    SELECT n FROM (SELECT id, ROW_NUMBER() OVER (ORDER BY created_at, id) AS n FROM servers) r WHERE r.id = servers.id
);
CREATE UNIQUE INDEX servers_number ON servers (number);
CREATE TABLE counters (
    name  TEXT NOT NULL PRIMARY KEY,
    value INTEGER NOT NULL CHECK (value >= 0)
);
INSERT INTO counters (name, value) SELECT 'server_number', COALESCE(MAX(number), 0) FROM servers;

-- +goose Down
DROP TABLE counters;
DROP INDEX servers_number;
ALTER TABLE servers DROP COLUMN number;
