-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Several game profiles per server, top first (SRV-008, D-074). The one
-- profile of a server of 1.0 becomes its only profile at position 0; the
-- column profile_id goes.
CREATE TABLE server_profiles (
    server_id  TEXT NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    profile_id TEXT NOT NULL REFERENCES game_profiles (id),
    position   INTEGER NOT NULL CHECK (position >= 0),
    PRIMARY KEY (server_id, profile_id),
    UNIQUE (server_id, position)
);
CREATE INDEX server_profiles_profile ON server_profiles (profile_id);
INSERT INTO server_profiles (server_id, profile_id, position)
    SELECT id, profile_id, 0 FROM servers WHERE profile_id IS NOT NULL;
ALTER TABLE servers DROP COLUMN profile_id;

-- +goose Down
ALTER TABLE servers ADD COLUMN profile_id TEXT REFERENCES game_profiles (id);
UPDATE servers SET profile_id = (
    SELECT sp.profile_id FROM server_profiles sp WHERE sp.server_id = servers.id ORDER BY sp.position LIMIT 1);
DROP TABLE server_profiles;
