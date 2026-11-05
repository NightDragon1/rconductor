# Installation

The [quick start](../../README.md#quick-start) sets up panel and agent on one
host. This page has the details. Every variable and setting is described in
[Configuration](configuration.md).

Run the panel only in your LAN or behind a VPN. No example here publishes an
RCON port, and none of yours should.

## Compose variants

| File | For |
| --- | --- |
| `deploy/compose/all-in-one.yaml` | panel and agent on one host; port 8443 stays inside the Compose network |
| `deploy/compose/panel.yaml` | the panel host of a separate setup; set `RCD_AGENT_PUBLIC_ADDR` to the address agents reach it under |
| `deploy/compose/agent.yaml` | each game host of a separate setup |

The files use `ghcr.io/nightdragon1/...:1`. The same tags are on Docker Hub as
`nightdragon1/rconductor-panel` and `nightdragon1/rconductor-agent`. Panel and
agent must have the same major version, the first number of the version; the
tag `1` gives you that. Versions have two parts: `1.0`, `1.1`. The first number
rises when something is incompatible.

Values come from the environment or an `.env` file next to the Compose file:
`DOCKER_GID` (group of the Docker socket), `RCD_PAIRING_TOKEN`, and for ports
`RCD_WEB_PORT` and `RCD_AGENT_PORT`.

To check the signature of an image:

```sh
cosign verify ghcr.io/nightdragon1/rconductor-panel:1 \
  --certificate-identity-regexp 'https://github.com/NightDragon1/rconductor/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## First start

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


## Reverse proxy

Set `RCD_BASE_PATH` when the panel runs below a path such as `/rcon`, and
`RCD_TRUSTED_PROXIES` to the addresses of your proxy: `X-Forwarded-For` and
`X-Forwarded-Proto` count only from those. For the WebSocket the proxy needs a
sufficient tunnel timeout. Never put a reverse proxy in front of the agent port
8443, its TLS ends at the panel.

## Volumes and files

All state lives in three volumes. Recreating the container with the same
volumes keeps all data. Keep `/secrets` separate from `/backups`: a backup
never contains the master key.

| Path | Content | Created |
| --- | --- | --- |
| `/data/rconductor.db` | SQLite database: users, servers, settings, audit log | first start |
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

## Agents

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

## Good to know

- **Copying over plain http:** browsers offer the clipboard only in a secure context. Over `http://lan-ip` the copy buttons select the text and ask for Ctrl+C; HTTPS through the reverse proxy gives direct copying.
- **Other RCON clients on Minecraft:** Minecraft collects the RCON output of all connections in one place. If two connections send at the same time, one gets the output of both or an empty answer ([#104](https://github.com/NightDragon1/rconductor/issues/104)). The panel uses one connection with commands in turn, but is affected as soon as a second client sends in parallel. Switch off old RCON containers such as itzg/rcon after moving to RCONductor, and expect single wrong or empty answers while backup containers such as itzg/mc-backup run (save-off, save-all), as with bots and rcon-cli scripts.

