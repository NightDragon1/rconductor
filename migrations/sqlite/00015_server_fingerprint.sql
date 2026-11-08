-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- The trusted certificate of a game server with an HTTPS API (PROT-003,
-- D-071): the SHA-256 of its certificate, lower-case hex; empty for RCON.
ALTER TABLE servers ADD COLUMN tls_fingerprint TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE servers DROP COLUMN tls_fingerprint;
