-- SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Display name of an agent's host, set by the RCON admin (UI-021, D-089);
-- NULL while none is set.
ALTER TABLE agents ADD COLUMN display_name TEXT;

-- +goose Down
ALTER TABLE agents DROP COLUMN display_name;
