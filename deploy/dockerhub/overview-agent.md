# RCONductor agent

The agent of RCONductor, the self-hosted web panel for RCON-capable game
servers (image `nightdragon1/rconductor-panel`). One agent runs on each game
host: it reads the Docker socket, reports the containers labelled
`rconductor.manage=true` with state, logs and usage to the panel, and starts,
stops or restarts them on the panel's order. It opens no port; it connects
out to the panel's agent port over TLS with pinned keys.

> **Status: pre-release.** Versions below 1.0 (tags `0.x`) are test
> versions with known issues. They are **not ready for production use**.
> There is no `latest` tag yet; the stable tag `1` follows with 1.0.

Run the panel only in your LAN or behind a VPN, and never publish the RCON
port of a game server. The agent needs the Docker socket; give it only to
this container.

Tags: `1` (newest 1.x) and the exact version such as `{version}`. Use the same
major version as the panel, and update the panel first.

## Quick start on a separate game host

```sh
curl -O https://raw.githubusercontent.com/NightDragon1/rconductor/main/deploy/compose/agent.yaml
echo "DOCKER_GID=$(stat -c %g /var/run/docker.sock)" > .env
echo 'RCD_PAIRING_TOKEN=rcd1....' >> .env
docker compose -f agent.yaml up -d
```

Make the pairing token in the panel under **Agents** → **Pair an agent** →
**Make token**. Until it has a token the agent waits and says so in its log.
The whole way, with pairing by fingerprint and firewall notes:
https://github.com/NightDragon1/rconductor/wiki/Agent-on-another-host

For panel and agent on one host, use the panel's Compose file
`all-in-one.yaml` (see the panel image).

## Important variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `DOCKER_GID` | `999` | group of `/var/run/docker.sock`; lets the agent reach the socket |
| `RCD_PAIRING_TOKEN` | empty | pairing token of the agent, made in the panel; ignored after the pairing |
| `RCD_PANEL_ADDR` | empty | `host:port` of the panel's agent port, for pairing by fingerprint or to override the token's address |
| `RCD_AGENT_NAME` | host name | name shown in the panel |
| `RCD_VERSION` | `1` | image tag in the Compose file, such as `1` or `{version}` |
| `RCD_IMAGE_REPO` | `docker.io/nightdragon1` | registry in the Compose file; `ghcr.io/nightdragon1` selects ghcr.io |

All variables: https://github.com/NightDragon1/rconductor/wiki/Configuration

## Links

- Source and known issues: https://github.com/NightDragon1/rconductor
- Documentation and examples: https://github.com/NightDragon1/rconductor/wiki
- Releases: https://github.com/NightDragon1/rconductor/releases
- License: AGPL-3.0-or-later
