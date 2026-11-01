# ShadowGhost.NET Game RCONductor

RCONductor is a self-hosted web panel for RCON-capable game servers with
fine-grained roles and permissions. Containers are controlled only by a small
agent per game host; the panel never touches the Docker socket.

> Status: under development (release 1 in progress). The foundation is in
> place: configuration, logging, database with migrations, master key,
> backups and the embedded web UI. Users, game servers and the agent follow.
> Compose files and the full operations guide come with release 1.0.0.

## Running the panel

The panel is one container with a web port (8080) and an agent port (8443,
not served yet). It starts without any configuration:

```sh
docker run -d --name rconductor-panel -p 8080:8080 \
  -v rcd-data:/data -v rcd-secrets:/secrets -v rcd-backups:/backups \
  rconductor-panel:dev
```

Released images will be published as `ghcr.io/nightdragon1/rconductor-panel`
and `ghcr.io/nightdragon1/rconductor-agent` for amd64 and arm64.

Run the panel only in your LAN or behind a VPN. The web port speaks plain
HTTP; TLS is the job of a reverse proxy.

## Volumes and files

All state lives in three volumes. Recreating the container with the same
volumes keeps all data. Keep `/secrets` separate from `/backups`: a backup
never contains the master key.

| Path | Content | Created |
| --- | --- | --- |
| `/data/rconductor.db` | SQLite database: settings and, later, users, servers, audit log | first start |
| `/data/rconductor.db-wal`, `/data/rconductor.db-shm` | SQLite write-ahead log and index, part of the database | while running |
| `/data/rconductor.db.migrate.lock` | lock against parallel migration runs | first start |
| `/secrets/master.key` | master key (32 random bytes, base64, mode 0600) for encrypting RCON credentials | first start, unless `RCD_MASTER_KEY_FILE` is set |
| `/backups/rconductor-<UTC time>-auto.db` | automatic backup, every 24 h, the last 7 are kept | 24 h after the newest automatic backup, or 24 h after start if there is none |
| `/backups/rconductor-<UTC time>-pre-migrate-v<N>.db` | backup before a schema migration from version N, the last 3 are kept | before each migration of an existing database |

While a backup is written it carries the suffix `.partial` and is renamed
when complete. Backup interval and retention are stored as settings in the
database. A
backup is a complete SQLite database; to restore it, stop the panel and copy
it to `/data/rconductor.db` (remove the `-wal` and `-shm` files first).

If the master key is lost, the panel starts, but RCON credentials have to be
entered again. Store a copy of `/secrets/master.key` in a safe place.

## Configuration

Only start-up and infrastructure are configured by environment variables;
everything that changes during operation is set in the panel. Variables
holding a secret also accept a `_FILE` variant that reads the value from a
file, for example a Docker secret.

| Variable | Default | Purpose |
| --- | --- | --- |
| `RCD_HTTP_ADDR` | `:8080` | address of the web UI and API |
| `RCD_AGENT_ADDR` | `:8443` | address of the agent port (TLS), served from the agent milestone |
| `RCD_AGENT_PUBLIC_ADDR` | empty | address under which agents reach the panel, written into pairing tokens |
| `RCD_PUBLIC_URL` | empty | public URL of the UI for invitation links and origin checks; with `https://` session cookies are always marked Secure |
| `RCD_BASE_PATH` | `/` | base path behind a reverse proxy |
| `RCD_TRUSTED_PROXIES` | empty | IP ranges of reverse proxies whose `X-Forwarded-For` and `X-Forwarded-Proto` are trusted; from anyone else these headers are ignored |
| `RCD_DB_DRIVER` | `sqlite` | database driver; `postgres` follows in release 2 |
| `RCD_DB_URL` / `_FILE` | empty | connection URL of an external Postgres (release 2) |
| `RCD_DATA_DIR` | `/data` | database and other data |
| `RCD_SECRETS_DIR` | `/secrets` | master key |
| `RCD_BACKUP_DIR` | `/backups` | backups |
| `RCD_MASTER_KEY_FILE` | empty | read the master key from this file instead of `/secrets/master.key`; it is never replaced |
| `RCD_EMERGENCY_ADMIN` | `true` | emergency admin enabled |
| `RCD_PASSWORD_HASH_CONCURRENCY` | `2` | password hashes computed at once (1 to 64); each takes 64 MiB of RAM |
| `RCD_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `RCD_LOG_FORMAT` | `text` | `text` or `json`; each audit-relevant event carries a fixed event code |

All variables are read and checked at start-up; an invalid configuration
stops the panel with exit code 2. Variables of features that are not built
yet are accepted but have no effect.

## Development

Requirements: Go (see `go.mod`), Node.js 22 or newer, Docker with buildx.

| Command | Does |
| --- | --- |
| `make web` | install UI dependencies and build the UI into `internal/webui/dist` |
| `make build` | static binaries `bin/panel` and `bin/agent` (CGO off) |
| `make test` | Go unit tests |
| `make images` | local images `rconductor-panel:dev` and `rconductor-agent:dev` |
| `make test-integration` | build the images and run the integration tests against them |
| `make lint`, `make vuln` | golangci-lint and govulncheck |
| `make generate` | regenerate sqlc and oapi-codegen code |
| `cd web && npm run lint && npm run format:check && npm test` | UI checks |

`api/openapi.yaml` is the single source of the API: server stubs are
generated with oapi-codegen, UI types with `npm run gen:api`. Without a UI
build the panel answers 503 on UI pages.

## License

RCONductor is free software, licensed under the
[GNU Affero General Public License v3.0 or later](LICENSE)
(SPDX: `AGPL-3.0-or-later`). Game profiles in `profiles/` are part of the
repository and covered by the same license.
