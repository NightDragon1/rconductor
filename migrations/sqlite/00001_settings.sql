-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Global settings and settings per server or server group (Design 4.2).
-- scope_id is '' for global settings so that the unique key also covers them.
CREATE TABLE settings (
    id         TEXT NOT NULL PRIMARY KEY,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    scope_type TEXT NOT NULL DEFAULT 'global'
        CHECK (scope_type IN ('global', 'server', 'server_group')),
    scope_id   TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL,
    CHECK ((scope_type = 'global') = (scope_id = '')),
    UNIQUE (key, scope_type, scope_id)
);

-- +goose Down
DROP TABLE settings;
