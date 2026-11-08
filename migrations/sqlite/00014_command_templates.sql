-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Command templates per server (CMD-008, D-068): a command with
-- placeholders such as {player}; the interface fills them and sends the
-- command through the pipeline. The name is unique per server among the
-- templates that are not deleted; a template is deleted softly, also with
-- its server.
CREATE TABLE command_templates (
    id         TEXT NOT NULL PRIMARY KEY,
    server_id  TEXT NOT NULL REFERENCES servers (id),
    name       TEXT NOT NULL,
    template   TEXT NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1,
    created_by TEXT NOT NULL REFERENCES users (id),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);
CREATE UNIQUE INDEX command_templates_name ON command_templates (server_id, name COLLATE NOCASE) WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE command_templates;
