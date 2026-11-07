# ShadowGhost.NET Game RCONductor

> **Pre-release, not for production use.** Versions below 1.0 (tags `0.x`)
> are test versions with known issues; there is no `latest` tag before 1.0.

RCONductor is a self-hosted web panel for RCON-capable game servers with
fine-grained roles and permissions. Containers are controlled only by a small
agent per game host; the panel never touches the Docker socket.

Run it only in your LAN or behind a VPN. The web port speaks plain HTTP; TLS
is the job of a reverse proxy. Never publish the RCON port of a game server.

## Quick start

Panel and agent on one host, with Docker Compose:

```sh
curl -O https://raw.githubusercontent.com/NightDragon1/rconductor/main/deploy/compose/all-in-one.yaml
echo "DOCKER_GID=$(stat -c %g /var/run/docker.sock)" > .env
docker compose -f all-in-one.yaml up -d
docker compose -f all-in-one.yaml logs panel | grep password:
```

`DOCKER_GID` is the group that owns the Docker socket on your host. The agent
runs as a non-root user and reaches the socket through this group, so
`stat -c %g /var/run/docker.sock` above reads the number for you.

1. Open `http://<host>:8080` and sign in as `admin` with the password from the
   log. You are asked for a new one; the initial password is shown only once.
2. Under **Agents**, choose **Pair an agent**, then **Make token**, and add the
   token to `.env`:

   ```sh
   echo 'RCD_PAIRING_TOKEN=rcd1....' >> .env
   docker compose -f all-in-one.yaml up -d agent
   ```

   Until it has a token the agent waits and says so in its log.
3. Give your game containers the label `rconductor.manage=true`; the agent manages
   only those.
4. Under **Servers and groups**, choose **New server**: name, game profile,
   host, RCON port and password, and optionally agent and container.

The images are on Docker Hub and ghcr.io (`nightdragon1/rconductor-panel`,
`nightdragon1/rconductor-agent`) for amd64 and arm64. The tag `1` follows all
1.x releases, so panel and agent stay compatible. Docker Hub is the default;
`RCD_IMAGE_REPO=ghcr.io/nightdragon1` in `.env` selects ghcr.io.

### Test versions

Before 1.0 the tag `1` does not exist yet, so a test version must be named. Do
this before the first `docker compose up`:

```sh
echo "RCD_VERSION=0.9" >> .env
```

A version `0.x` is a pre-release for testing, with known issues: they are listed
in its [release notes](https://github.com/NightDragon1/rconductor/releases). It
has only its exact tag, there is no `0` or `latest`.

## Volumes and files

All state lives in three volumes. Recreating the container with the same
volumes keeps all data. Keep `/secrets` separate from `/backups`: a backup
never contains the master key.

| Path | Content | Created |
| --- | --- | --- |
| `/data/rconductor.db` | SQLite database: users, servers, settings, audit log | first start |
| `/data/rconductor.db-wal`, `/data/rconductor.db-shm` | SQLite write-ahead log and index, part of the database | while running |
| `/data/rconductor.db.migrate.lock` | lock against parallel migration runs | first start |
| `/data/profiles/*.yaml` | custom game profiles; one with the id of a shipped profile replaces it; changes are picked up without restart (file events, plus a check every 30 s, setting `profiles.poll_interval`); check one first with `docker run --rm -v ./x.yaml:/p.yaml:ro nightdragon1/rconductor-panel:0.10 profile check /p.yaml` ([details](docs/user/technical.md#checking-a-profile)) | by you |
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

## Documentation

| Read this | For |
| --- | --- |
| [Overview](docs/user/overview.md) | the idea, panel and agent, why there is an agent |
| [Installation](docs/user/installation.md) | both Compose variants, agents, reverse proxy, volumes, backups |
| [Configuration](docs/user/configuration.md) | every environment variable and setting |
| [Roles and rights](docs/user/roles.md) | who may do what, and how rights are assigned |
| [Interface](docs/user/interface.md) | how the web interface is built and used |
| [Technical notes](docs/user/technical.md) | profiles and lists, pairing, keys, encryption |

Releases are listed in the [changelog](CHANGELOG.md).

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
| `make test-e2e` | build the UI and run the Playwright tests against a test panel (`internal/e2e/stack`) |
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
