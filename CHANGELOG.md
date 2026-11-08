# Changelog

## 1.1

- Wrappers are also refused with a namespace prefix such as minecraft:execute (CMD-005).
- The panel works with older agents of the same major version and hides what they cannot do; update the panel before the agents (AGT-013).
- The game version from the container logs shows on the cards and the server page, for every role that sees the server (AGT-006).
- The used size of each volume is measured in the background; the API gives it with its age (AGT-007).

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
