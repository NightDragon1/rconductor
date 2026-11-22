# Changelog

## 1.3

### Changed
- Deleting a user deletes their user groups and command lists too; the dialog names them, the audit lists them (AUTH-002, ROLE-005).
- Two-factor sign-in for everyone (auth.totp_required) can be switched on only by an RCON admin with an own second factor; otherwise the panel refuses with a hint (AUTH-008).
- A container belongs to one server only; a second server on it is refused. Update note: migration 00024 releases double bindings; the oldest server keeps the container, the others lose agent and container, each with an audit entry and a log line db.migrate.binding_released (SRV-001).
- The functions of 1.2 can no longer be switched off; Settings has no group Features. API: the field features is gone from the settings and the session (ui.features), PATCH /settings refuses the keys features.<name>; stored values stay unused (NFR-019).
- The sign-in and profile field is called Username in the interface and the docs; the API field stays pseudonym (AUTH-009).
- A multi-server command answers in time: the first server always runs, servers whose turn comes too late are skipped with reason time_budget and their own audit entry (CMD-010).

### Fixed
- Messages and comments: Load more goes on after a deleted item and is not offered after a last page of exactly 50; API: cursor and next_cursor, before and after are deprecated (MSG-001, MSG-003).
- A broken RCON connection redials at once, a profile change reconnects at once, and a reload no longer waits for running commands (PROT-001, PROT-003).
- A say prefix draft survives a change by someone else and shows the conflict; max_length 0 in a profile is refused (CMD-020, CAT-006).

### Security
- The built-in list blacks out the system log and the diagnostic export also while the masking is off; more secret forms are masked, domains and the mail server pseudonymized (UI-022, NFR-018).
- Rights, scopes and the say prefix are checked again inside the writing transaction (ROLE-003, ROLE-004).
- A new role or a lock of a moderator ends the sessions of their supporters; a setting that cannot be read keeps the protection on, with a log (ROLE-006, AUTH-006).

## 1.2

### New
- Messages in the panel as conversations, with comments and unread count (MSG-001 to MSG-003).
- System log page and a diagnostic export as ZIP for the RCON admin, blacked out (UI-025, NFR-018).
- Secrets masked in container logs; the RCON admin adds own texts (UI-022).
- say with the sender's name and a prefix per server group (CMD-020).
- Game profile browser with list and details, command palette, Minecraft color codes as styles (UI-020, CMD-016, CMD-017).
- Overview grouped by host, server number, Docker host for the RCON admin, Save as New, two-part log filter (UI-021, UI-018, AGT-014, UI-017, UI-019).
- About with tabs, version line and the full third-party list for Go and UI; versions and licenses in the images (UI-024, AGT-016, NFR-021).
- Container health (healthy, starting, unhealthy) when a container has a healthcheck; paused, restarting and dead shown by name (UI-021, UI-011).

### Changed
- The new functions are on; the RCON admin can switch each off under Settings, Features (NFR-019).
- Sage accent and corner frames; Servers has the tabs Cards and Manage servers and server groups, the old entry under Administration is gone and its address leads there; topology as a tree panel → host → server (UI-021, UI-011, UI-010).
- Settings in groups with Advanced, dense tables from 1024 px, open invitations in the user list with Withdraw (UI-014, UI-007, UI-008).
- A say on Minecraft goes out as tellraw with the sender (CMD-020).
- Update the panel before the agents; agents 1.1 keep working, Docker host, agent log and container health need agent 1.2 (AGT-013, AGT-014, UI-025).

### Fixed
- The password change comes before the setup of the second factor (AUTH-010).
- Logo with enough contrast in the light design (UI-023).

### Security
- An unreadable security setting no longer opens the protection: the two-factor duty and limits keep their last or default value (AUTH-008).
- Scope and grant targets are checked in the same transaction as the write (ROLE-003, ROLE-004).
- Game ports in the profile list only for admins (UI-005).
- Updated dependencies.

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
