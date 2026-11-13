# Changelog

## 1.1

### New
- Two-factor sign-in (TOTP) with recovery by mail; mail over SMTP (AUTH-008, NFR-017).
- Command templates and one command to several servers at once (CMD-008, CMD-010).
- Several game profiles per server, the top one wins; profiles are generic or specific (SRV-008, CAT-007).
- Game servers with an HTTPS API, first Satisfactory, with certificate pinning (PROT-003).
- Game version, volume sizes, ports and volumes on cards and server page; topology page (AGT-006, AGT-007, UI-005, UI-010).
- History and live console visible by role (ROLE-009).
- 29 more shipped game profiles.

### Changed
- API: `profile_id` is replaced by the list `profile_ids`; scripts must change (SRV-008).
- Profiles using the new fields (`kind`, `ports.steam_connect`, `researched_version`, `sources`, `status`) fail the profile check of panel 1.0.
- Update the panel before the agents; agents 1.x keep working (AGT-013).
- Server admins see game ports only; host, RCON port and Steam link are for the RCON admin (UI-005).
- Deleting a server or group also removes the assignments that name it.

### Fixed
- Factorio answers commands; answers over 64 KiB no longer shift later answers.
- Second-factor code and sign-in in one step; a sign-in ends the session sent along.
- Theme switch, server status after an agent restart, leaving a page while saving.

### Security
- Go 1.27.4 (net/http fix); rate limits per IPv6 /64 with an overflow limit; stricter profile check; wrappers also refused with a namespace prefix.

## 1.0

First release.

Coming from a test version: the container label is now `rconductor.manage=true`. Containers that only have the old label `rcon.manage=true` are no longer managed, so change the label before you update.

- Local users with roles (RCON admin, server admin, moderator, supporter), groups, invitations and an emergency admin.
- Game servers over Source RCON, game profiles with a command catalog, command lists per moderator group.
- Console for admins, command page with fields for moderators and supporters, history, audit log.
- Host agent for Docker: status, metrics, logs, start, stop and restart of labeled containers, paired with pinned keys.
- Web interface in dark and light design, also on a phone.
- Signed images for amd64 and arm64 on ghcr.io and Docker Hub, with SBOM.
- `profile check` in the panel image checks your own game profiles before you add them.

### Known issues

- Undo of assignments and of mass actions compares first and writes afterwards: a change by a second admin in between can be overwritten. See #8.
- The player list is cut at about 230 Minecraft names.
- A change refused because the session has ended shows an error instead of the sign-in page; reload the page to sign in again.
- A custom game profile near the size limit that repeats large parts through aliases, or that has very many unknown keys, can take about a second and more than 100 MB to import.
