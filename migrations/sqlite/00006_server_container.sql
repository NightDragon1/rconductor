-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- A server refers to its container by name: Docker gives a container a new
-- ID whenever it is created again, the name stays (revision 2.3
-- candidate 1). The agent resolves the ID and checks the label each time.
ALTER TABLE servers RENAME COLUMN container_id TO container_name;

-- +goose Down
ALTER TABLE servers RENAME COLUMN container_name TO container_id;
