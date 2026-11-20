-- name: InsertSystemLog :exec
INSERT INTO system_log (ts, level, code, text, size) VALUES (?, ?, ?, ?, ?);

-- name: PurgeSystemLogBefore :execrows
DELETE FROM system_log WHERE ts < ?;

-- name: SystemLogBytes :one
SELECT CAST(coalesce(sum(size), 0) AS INTEGER) FROM system_log;

-- name: DeleteOldestSystemLog :execrows
DELETE FROM system_log WHERE id IN (SELECT id FROM system_log ORDER BY id LIMIT ?);

-- name: ListSystemLog :many
-- From a minimum level up (info 0, warn 1, error 2), oldest first, after an ID.
SELECT id, ts, level, code, text FROM system_log
WHERE ts >= sqlc.arg(from_ts) AND ts < sqlc.arg(to_ts) AND id > sqlc.arg(after)
  AND (CASE level WHEN 'error' THEN 2 WHEN 'warn' THEN 1 ELSE 0 END) >= CAST(sqlc.arg(min_rank) AS INTEGER)
ORDER BY id
LIMIT sqlc.arg(max_rows);

-- SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
-- SPDX-License-Identifier: AGPL-3.0-or-later
