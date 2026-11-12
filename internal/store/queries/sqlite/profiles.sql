-- Imported game profiles and their catalog (CAT-001, CAT-003).

-- name: GetProfileBySlug :one
SELECT id, slug, content_hash, overridden FROM game_profiles WHERE slug = ?;

-- name: CountProfileServers :one
-- Servers that have the profile on their list (SRV-008).
SELECT count(*) FROM server_profiles sp JOIN servers s ON s.id = sp.server_id
WHERE sp.profile_id = ? AND s.deleted_at IS NULL;

-- name: InsertGameProfile :exec
INSERT INTO game_profiles (id, slug, name, protocol, default_port, rules, source_file, content_hash, overridden, imported_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateGameProfile :exec
UPDATE game_profiles
SET name = ?, protocol = ?, default_port = ?, rules = ?, source_file = ?, content_hash = ?, overridden = ?,
    imported_at = ?, last_error = NULL, last_error_at = NULL
WHERE id = ?;

-- name: SetProfileError :exec
UPDATE game_profiles SET last_error = ?, last_error_at = ? WHERE slug = ?;

-- name: DeleteCatalog :exec
DELETE FROM catalog_commands WHERE profile_id = ?;

-- name: InsertCatalogCommand :exec
INSERT INTO catalog_commands (id, profile_id, position, path, description, params) VALUES (?, ?, ?, ?, ?, ?);

-- name: ListGameProfiles :many
SELECT id, slug, name, protocol, default_port, rules, source_file, overridden, imported_at, last_error, last_error_at
FROM game_profiles ORDER BY name COLLATE NOCASE;

-- name: ListCatalog :many
SELECT profile_id, path, description, params FROM catalog_commands ORDER BY profile_id, position;

-- name: ListCatalogOfProfile :many
SELECT path, description, params FROM catalog_commands WHERE profile_id = ? ORDER BY position;

-- SPDX-License-Identifier: AGPL-3.0-or-later
