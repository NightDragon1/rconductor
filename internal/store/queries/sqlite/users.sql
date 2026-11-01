-- name: GetEmergencyAdmin :one
SELECT id, pseudonym FROM users WHERE is_emergency = 1;

-- name: InsertUser :exec
INSERT INTO users (
    id, pseudonym, role, password_hash, is_emergency, created_by, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- SPDX-License-Identifier: AGPL-3.0-or-later
