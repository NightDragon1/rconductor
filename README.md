# ShadowGhost.NET Game RCONductor

RCONductor is a self-hosted web panel for RCON-capable game servers with
fine-grained roles and permissions. Containers are controlled only by a small
agent per game host; the panel never touches the Docker socket.

> Status: under development (release 1 in progress). Users and rights,
> game servers with RCON, the console over the API and the host agent work;
> the web UI follows. Compose files and the full operations guide come with
> release 1.0.0.

## Running the panel

The panel is one container with a web port (8080) and an agent port
(8443). It starts without any configuration:

```sh
docker run -d --name rconductor-panel -p 8080:8080 \
  -v rcd-data:/data -v rcd-secrets:/secrets -v rcd-backups:/backups \
  rconductor-panel:dev
```

Released images will be published as `ghcr.io/nightdragon1/rconductor-panel`
and `ghcr.io/nightdragon1/rconductor-agent` for amd64 and arm64.

On the first start the panel creates the emergency admin `admin` and
prints a random initial password once to the container log
(`docker logs rconductor-panel`). The password stays readable in the
container logs as long as the container and its logs exist. You must
change it at your first login (profile, change password); the initial
password is invalid from then on.

Lost the emergency admin's password? Start the panel once with
`RCD_EMERGENCY_ADMIN_RESET=true`: it prints a new initial password the same
way, ends all sessions of the emergency admin and lifts a lock. Remove the
variable afterwards, otherwise every start resets the password again.

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
| `/data/profiles/*.yaml` | custom game profiles; one with the id of a shipped profile replaces it; changes are picked up without restart (file events, plus a check every 30 s, setting `profiles.poll_interval`) | by you |
| `/secrets/master.key` | master key (32 random bytes, base64, mode 0600) for encrypting RCON credentials | first start, unless `RCD_MASTER_KEY_FILE` is set |
| `/secrets/panel.key` | Ed25519 key pair of the agent port (PEM, mode 0600); agents pin its fingerprint, shown as `panel_fingerprint` in the `app.ready` line. Losing it means pairing all agents again | first start |
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
| `RCD_AGENT_ADDR` | `:8443` | address of the agent port (TLS 1.3 with pinned keys); never put a reverse proxy in front of it |
| `RCD_AGENT_PUBLIC_ADDR` | empty | address under which agents reach the panel, written into pairing tokens |
| `RCD_PUBLIC_URL` | empty | public URL of the UI for invitation links and origin checks; with `https://` session cookies are always marked Secure |
| `RCD_BASE_PATH` | `/` | base path behind a reverse proxy |
| `RCD_TRUSTED_PROXIES` | empty | IP ranges of reverse proxies whose `X-Forwarded-For` and `X-Forwarded-Proto` are trusted; from anyone else these headers are ignored |
| `RCD_DB_DRIVER` | `sqlite` | database driver; `postgres` follows in release 2 |
| `RCD_DB_URL` / `_FILE` | empty | connection URL of an external Postgres (release 2) |
| `RCD_DATA_DIR` | `/data` | database and other data |
| `RCD_SECRETS_DIR` | `/secrets` | master key and panel key |
| `RCD_BACKUP_DIR` | `/backups` | backups |
| `RCD_MASTER_KEY_FILE` | empty | read the master key from this file instead of `/secrets/master.key`; it is never replaced |
| `RCD_EMERGENCY_ADMIN` | `true` | emergency admin enabled |
| `RCD_EMERGENCY_ADMIN_RESET` | `false` | `true`: on start, give the emergency admin a new initial password and end his sessions; remove it again afterwards |
| `RCD_PASSWORD_HASH_CONCURRENCY` | `2` | password hashes computed at once (1 to 64); each takes 64 MiB of RAM |
| `RCD_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `RCD_LOG_FORMAT` | `text` | `text` or `json`; each audit-relevant event carries a fixed event code |

All variables are read and checked at start-up; an invalid configuration
stops the panel with exit code 2. Variables of features that are not built
yet are accepted but have no effect.

## Running an agent

One agent runs on each game host. It is the only part with access to the
Docker socket, connects out to the panel's agent port (it opens no port
itself) and manages only containers with the label `rcon.manage=true`:
start, stop, restart, status, metrics and logs. It cannot create, remove
or change containers, run commands in them or touch images and volumes.

Pairing needs the RCON admin. Either create a pairing token in the panel
(`POST /api/v1/agents/tokens`, valid once for 24 hours); the answer
contains a ready Compose service:

```yaml
services:
  rconductor-agent:
    image: ghcr.io/nightdragon1/rconductor-agent:1
    restart: unless-stopped
    group_add: ["DOCKER_GID"]   # stat -c %g /var/run/docker.sock
    environment:
      RCD_PAIRING_TOKEN: "rcd1...."
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - rconductor-agent-secrets:/secrets
volumes:
  rconductor-agent-secrets:
```

Or, without a token, set only `RCD_PANEL_ADDR` and switch on pairing by
fingerprint in the panel (setting `agents.fingerprint_pairing`; it closes
itself after 30 minutes). The agent then writes its own fingerprint and the
panel's to its log; compare them with the RCON admin, who approves the
agent (`POST /api/v1/agents/{id}/approve`). The agent pins the panel key
and keeps its pairing in `/secrets`; the token is ignored afterwards and
may stay in the file. After a revocation the agent stops reconnecting
until it is paired again with a new token or by fingerprint.

The agent runs as a non-root user (65532) and reaches the socket through
the socket's group (`group_add` with its GID). Be aware that access to the
Docker socket is equivalent to root on the host: non-root only limits what
a flaw in the agent process itself can do. A socket proxy in front of the
socket narrows the API further (see below).

| Variable | Default | Purpose |
| --- | --- | --- |
| `RCD_PAIRING_TOKEN` / `_FILE` | empty | pairing token (way A) |
| `RCD_PANEL_ADDR` | empty | panel agent port, `host:port`; for way B or to override the token's address |
| `RCD_AGENT_NAME` | host name | name shown in the panel |
| `RCD_LABEL` | `rcon.manage=true` | label that allows a container |
| `RCD_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker API, also a socket proxy such as `tcp://socket-proxy:2375` |
| `RCD_SECRETS_DIR` | `/secrets` | agent key (`agent.key`) and pairing (`panel.json`) |
| `RCD_LOG_LEVEL`, `RCD_LOG_FORMAT` | `info`, `text` | as for the panel |

### Agent behind a socket proxy

A socket proxy lets the agent reach only the parts of the Docker API it
needs: listing and inspecting containers, events, logs, stats, start, stop
and restart. This example uses
[linuxserver/socket-proxy](https://github.com/linuxserver/docker-socket-proxy)
(GPL-3.0, a separate image); the integration tests run every agent function
through it.

```yaml
services:
  socket-proxy:
    image: lscr.io/linuxserver/socket-proxy:3.4.7   # pin by digest in production
    restart: unless-stopped
    read_only: true
    tmpfs: [/run]
    environment:
      CONTAINERS: "1"      # list, inspect, logs and stats of containers
      EVENTS: "1"          # status changes
      ALLOW_START: "1"
      ALLOW_STOP: "1"
      ALLOW_RESTARTS: "1"  # also allows kill, which the agent never calls
      ALLOW_LOGS: "1"
      POST: "0"            # no other changing call: no create, exec, images, volumes
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro

  rconductor-agent:
    image: ghcr.io/nightdragon1/rconductor-agent:1
    restart: unless-stopped
    environment:
      RCD_PAIRING_TOKEN: "rcd1...."
      RCD_DOCKER_HOST: tcp://socket-proxy:2375
    volumes:
      - rconductor-agent-secrets:/secrets
    depends_on: [socket-proxy]

volumes:
  rconductor-agent-secrets:
```

The agent then needs neither the socket nor `group_add`. Do not publish the
proxy's port; only the agent may reach it.

## Development

Requirements: Go (see `go.mod`), Node.js 22 or newer, Docker with buildx.

| Command | Does |
| --- | --- |
| `make web` | install UI dependencies and build the UI into `internal/webui/dist` |
| `make build` | static binaries `bin/panel` and `bin/agent` (CGO off) |
| `make test` | Go unit tests |
| `make images` | local images `rconductor-panel:dev` and `rconductor-agent:dev` |
| `make test-integration` | build the images and run the integration tests against them |
| `MINECRAFT_ACCEPT_EULA=true make test-minecraft` | walking skeleton against a real Minecraft server (see below) |
| `make lint`, `make vuln` | golangci-lint and govulncheck |
| `make generate` | regenerate sqlc and oapi-codegen code |
| `cd web && npm run lint && npm run format:check && npm test` | UI checks |

`api/openapi.yaml` is the single source of the API: server stubs are
generated with oapi-codegen, UI types with `npm run gen:api`. Without a UI
build the panel answers 503 on UI pages.

`make test-minecraft` downloads the pinned Minecraft server JAR into
`.cache/minecraft` (checked against its SHA-1) and runs it in a Java
container. Running a Minecraft server means accepting the
[Minecraft EULA](https://aka.ms/MinecraftEULA). The script and the test
refuse to run until you accept it by setting `MINECRAFT_ACCEPT_EULA=true`;
only then is `eula=true` written for the server. CI sets the variable for
its Minecraft job.

## License

RCONductor is free software, licensed under the
[GNU Affero General Public License v3.0 or later](LICENSE)
(SPDX: `AGPL-3.0-or-later`). Game profiles in `profiles/` are part of the
repository and covered by the same license.
