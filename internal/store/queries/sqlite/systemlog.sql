-- name: InsertSystemLog :exec
INSERT INTO system_log (ts, level, code, text, size) VALUES (?, ?, ?, ?, ?);

-- name: PurgeSystemLogBefore :execrows
DELETE FROM system_log WHERE ts < ?;

-- name: SystemLogBytes :one
SELECT CAST(coalesce(sum(size), 0) AS INTEGER) FROM system_log;

-- name: DeleteOldestSystemLog :execrows
DELETE FROM system_log WHERE id IN (SELECT id FROM system_log ORDER BY id LIMIT ?);

-- SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
-- SPDX-License-Identifier: AGPL-3.0-or-later
