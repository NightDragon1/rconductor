# ShadowGhost.NET Game RCONductor

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

1. Open `http://<host>:8080` and sign in as `admin` with the password from the
   log. You are asked for a new one; the initial password is shown only once.
2. Under **Agents**, choose **Pair an agent**, then **Make token**, and add the
   token to `.env`:

   ```sh
   echo 'RCD_PAIRING_TOKEN=rcd1....' >> .env
   docker compose -f all-in-one.yaml up -d agent
   ```

   Until it has a token the agent waits and says so in its log.
3. Give your game containers the label `rcon.manage=true`; the agent manages
   only those.
4. Under **Servers and groups**, choose **New server**: name, game profile,
   host, RCON port and password, and optionally agent and container.

The images are on ghcr.io and Docker Hub (`nightdragon1/rconductor-panel`,
`nightdragon1/rconductor-agent`) for amd64 and arm64. The tag `1` follows all
1.x releases, so panel and agent stay compatible.

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
