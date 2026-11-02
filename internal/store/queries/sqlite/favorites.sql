-- Favorite servers per user (CMD-001).

-- name: ListFavorites :many
SELECT f.server_id FROM user_favorites f
JOIN servers s ON s.id = f.server_id AND s.deleted_at IS NULL
WHERE f.user_id = ? ORDER BY f.position;

-- name: DeleteFavorites :exec
DELETE FROM user_favorites WHERE user_id = ?;

-- name: InsertFavorite :exec
INSERT INTO user_favorites (user_id, server_id, position) VALUES (?, ?, ?);

-- SPDX-License-Identifier: AGPL-3.0-or-later
