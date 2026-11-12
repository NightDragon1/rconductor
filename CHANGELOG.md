# Changelog

## 1.1

- Wrappers are also refused with a namespace prefix such as minecraft:execute (CMD-005).
- The panel works with older agents of the same major version and hides what they cannot do; update the panel before the agents (AGT-013).
- The game version from the container logs shows on the cards and the server page, for every role that sees the server (AGT-006).
- The used size of each volume is measured in the background; the API gives it with its age (AGT-007).
- Ports and volumes on the cards and the server page, with a Steam connect link where the game profile sets `ports.steam_connect` (UI-005). Game profiles with `ports.steam_connect` fail the profile check of panel 1.0.
- Topology page: host → agent → game server by server group, for RCON admin and server admin (UI-010).
- Mail over SMTP: the RCON admin sets up host, port, encryption (STARTTLS, SSL/TLS or none), user, password and sender and sends a test mail from the settings (NFR-017).
- Two-factor sign-in with an authenticator app (TOTP), optional for everyone or required by the RCON admin; recovery by mail to the address in the profile, or a reset by whoever manages the user (AUTH-008).
- Visibility of commands in the history and the live console is fixed by role: moderators and supporters now see the commands of the other moderators and supporters on their servers; server admins no longer see those of RCON admins. A switch in the history shows only your own (ROLE-009).
- Command templates with placeholders per server, such as `kick {player} {reason}`: RCON admin and server admin keep them in the console, moderators and supporters use those of their list on the command page; a filled template is checked like a typed command (CMD-008).
- One command to several servers at once from the console, for RCON admin and server admin within their scope; each server checks and answers on its own and counts against its own rate limit (CMD-010).
- Game profiles name their kind, `generic` or `specific` (a modpack or plugin above a game); without it a profile is generic. Profiles with `kind` fail the profile check of panel 1.0 (CAT-007).
- The Minecraft profile treats `function`, `return` and `schedule` as wrappers like `execute`: moderators and supporters cannot send them (CMD-005).
- 29 more shipped game profiles: ARK, Conan Exiles, Factorio, Insurgency: Sandstorm, Mordhau, Palworld, Project Zomboid, Squad, Starbound, V Rising, Source games, Minecraft Forge, NeoForge, Paper and 1.12.2, and Minecraft mods and plugins; the list is in the technical notes (CAT-001, DOC-005).
- Game profiles name their origin: `researched_version`, `sources` and `status` (`tested` or `untested`, default `untested`); the shipped profiles use them instead of source comments. Profiles with these fields fail the profile check of panel 1.0 (CAT-001, CAT-007, DOC-005).
- A command that the game runs and reports as failed (Satisfactory) shows as error with the game's text; the single-command API gives `result` and `reason` (`command_failed`) like history and multi-command (PROT-003).
- Servers with an HTTPS API: the end of validity of the trusted certificate is stored and given with the server and the connection test; the trust holds regardless of it. The technical notes describe renewal and trusting again (PROT-003).
- A server has an ordered list of game profiles, such as a modpack above its game: for each value the topmost profile that sets it wins, the command catalog is the union, wrappers of all profiles hold. All profiles need the same protocol. The server admin may change the list on servers in his scope. Servers of panel 1.0 keep their profile as the only entry (SRV-008, CAT-007).
- API: the server field `profile_id` is replaced by the list `profile_ids`, top first; scripts that use `profile_id` must change (SRV-008).
- Fixed: Factorio servers answered no command, because Factorio refuses the empty packet that marks the end of an answer; the panel ran into its timeout and reconnected. Game profiles now choose the end of an answer with `source_rcon.end_marker` (`empty_response`, the default, or `none`); the Factorio profile sets `none`. Panel 1.0 had the same problem with own Factorio profiles. Profiles with `source_rcon` fail the profile check of panel 1.0 (PROT-001).

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
