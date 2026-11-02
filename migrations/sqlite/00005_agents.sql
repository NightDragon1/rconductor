-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Paired agents and pairing tokens (Design 1.2, 4.2; M3).

-- An agent is pending while a pairing by fingerprint (way B) waits for the
-- RCON admin, active once paired, revoked after a revocation (AGT-012).
-- public_key is the raw Ed25519 key the agent shows in the TLS handshake;
-- fingerprint is its SHA-256 as people compare it. paired_by is empty for a
-- pending agent.
CREATE TABLE agents (
    id           TEXT NOT NULL PRIMARY KEY,
    name         TEXT NOT NULL,
    public_key   BLOB NOT NULL,
    fingerprint  TEXT NOT NULL UNIQUE,
    status       TEXT NOT NULL CHECK (status IN ('pending', 'active', 'revoked')),
    version      TEXT NOT NULL DEFAULT '',
    capabilities TEXT NOT NULL DEFAULT '[]',
    remote       TEXT NOT NULL DEFAULT '',
    paired_by    TEXT REFERENCES users (id),
    created_at   TEXT NOT NULL,
    last_seen_at TEXT,
    revoked_at   TEXT,
    CHECK ((status = 'revoked') = (revoked_at IS NOT NULL))
);

-- Pairing tokens (way A): only the SHA-256 of the secret is stored; a token
-- is valid once until expires_at (24 h).
CREATE TABLE agent_tokens (
    id         TEXT NOT NULL PRIMARY KEY,
    token_hash BLOB NOT NULL UNIQUE,
    created_by TEXT NOT NULL REFERENCES users (id),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    used_at    TEXT,
    agent_id   TEXT REFERENCES agents (id),
    CHECK ((used_at IS NULL) = (agent_id IS NULL))
);

-- +goose Down
DROP TABLE agent_tokens;
DROP TABLE agents;
