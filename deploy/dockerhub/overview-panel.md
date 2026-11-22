# RCONductor panel

[![CI](https://img.shields.io/github/actions/workflow/status/NightDragon1/rconductor/ci.yml?branch=main&label=CI)](https://github.com/NightDragon1/rconductor/actions/workflows/ci.yml?query=branch%3Amain)
[![Release](https://img.shields.io/github/v/release/NightDragon1/rconductor)](https://github.com/NightDragon1/rconductor/releases/latest)
[![License AGPL-3.0-or-later](https://img.shields.io/badge/license-AGPL--3.0--or--later-blue)](https://github.com/NightDragon1/rconductor/blob/main/LICENSE)
![Platforms amd64 and arm64](https://img.shields.io/badge/platforms-amd64%20%7C%20arm64-informational)

![ShadowGhost.NET RCONductor](https://raw.githubusercontent.com/NightDragon1/rconductor/main/web/src/assets/logo/logo.svg)

Self-hosted web panel for RCON-capable game servers, with roles and
permissions. Containers are controlled by a small agent per game host
(image `nightdragon1/rconductor-agent`); the panel never touches the Docker
socket.

> **Status: pre-release.** Versions below 1.0 (tags `0.x`) are test
> versions with known issues. They are **not ready for production use**.
> There is no `latest` tag yet; the stable tag `1` follows with 1.0.

<!-- security note of README.md -->

![Architecture: browser, reverse proxy, panel on 8080; the agent connects to the panel's agent port 8443 over TLS and reaches only labelled containers through the Docker socket; the panel talks to game servers over RCON or an HTTPS API](https://raw.githubusercontent.com/NightDragon1/rconductor/main/assets/architecture.svg)

Tags: `1` (newest 1.x) and the exact version such as `{version}`. There is no `latest`; use `:1`.

<!-- quick start of README.md -->

<!-- important variables of README.md -->

## Links

- Source and known issues: https://github.com/NightDragon1/rconductor
- Documentation: https://github.com/NightDragon1/rconductor/tree/main/docs/user
- Wiki with examples: https://github.com/NightDragon1/rconductor/wiki
- Releases: https://github.com/NightDragon1/rconductor/releases
