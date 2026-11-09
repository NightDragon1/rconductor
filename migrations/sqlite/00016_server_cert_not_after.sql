-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- End of validity of the pinned certificate of a game server with an
-- HTTPS API (D-072, D-073), RFC 3339 in UTC; NULL until known. It is set
-- when the pin is saved and the server shows that certificate, or at the
-- first connection with the pin; a new pin clears it. Shown only, the pin
-- does not depend on it.
ALTER TABLE servers ADD COLUMN tls_not_after TEXT;

-- +goose Down
ALTER TABLE servers DROP COLUMN tls_not_after;
