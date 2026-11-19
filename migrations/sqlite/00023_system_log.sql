-- SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- System log of the panel (D-091, UI-025): its events from info up,
-- blacked out and masked before writing, kept 7 days and up to
-- system_log.max_bytes in all (oldest first out). size is the length of
-- text in bytes.
CREATE TABLE system_log (
    id    INTEGER PRIMARY KEY AUTOINCREMENT,
    ts    TEXT    NOT NULL,
    level TEXT    NOT NULL,
    code  TEXT    NOT NULL DEFAULT '',
    text  TEXT    NOT NULL,
    size  INTEGER NOT NULL
);
CREATE INDEX system_log_ts ON system_log (ts);

-- +goose Down
DROP TABLE system_log;
