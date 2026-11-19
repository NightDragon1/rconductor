-- name: PurgeMessages :execrows
-- Recipients and comments go with the message (ON DELETE CASCADE, D-086).
DELETE FROM messages WHERE created_at < ?;

-- SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
-- SPDX-License-Identifier: AGPL-3.0-or-later
