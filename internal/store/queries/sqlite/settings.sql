-- name: GetSetting :one
SELECT value FROM settings
WHERE key = ? AND scope_type = ? AND scope_id = ?;

-- name: GetSettingWithTime :one
SELECT value, updated_at FROM settings
WHERE key = ? AND scope_type = 'global' AND scope_id = '';

-- name: InsertSettingIfMissing :execrows
INSERT INTO settings (id, key, value, scope_type, scope_id, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (key, scope_type, scope_id) DO NOTHING;

-- SPDX-License-Identifier: AGPL-3.0-or-later
