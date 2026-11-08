-- name: GetSMTPSettings :one
SELECT host, port, encryption, username, password, sender, version, updated_at FROM smtp_settings WHERE id = 1;

-- name: UpdateSMTPSettings :execrows
UPDATE smtp_settings SET host = ?, port = ?, encryption = ?, username = ?, password = ?, sender = ?,
    version = version + 1, updated_at = ?
WHERE id = 1 AND version = ?;

-- SPDX-License-Identifier: AGPL-3.0-or-later
