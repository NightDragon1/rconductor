# Technical notes

## Game profiles

A game profile is a YAML file with the rules of a game: syntax, ports and a command
catalog. It allows and forbids nothing; it provides completion, descriptions and the
fields of the command page.

| Topic | Behaviour |
| --- | --- |
| Storage | shipped profiles are built into the panel image (`profiles/` in the repository); your own go to `/data/profiles/*.yaml` |
| Loading | changes arrive by file events; in addition the panel compares files every `profiles.poll_interval`, because network drives send no events |
| Precedence | your profile with the id of a shipped one replaces it and is marked as adapted |
| Kind | `kind: generic` for a game as such, `kind: specific` for a modpack or plugin that ranks above it; a profile without `kind` is generic. Profiles inherit nothing from each other (1.1) |
| Errors | an invalid file is not imported; the last valid state stays active, the error is in the log and the health overview |
| Aliases | a file has at most 100 YAML aliases |
| Anchors | an anchored value must not contain aliases |
| Nesting | lists and mappings nest at most 64 levels deep |

### Checking a profile

Check a profile before you copy it to `/data/profiles`. The panel image does it
without configuration, database or network:

```sh
docker run --rm -v ./my-game.yaml:/p.yaml:ro nightdragon1/rconductor-panel:1 profile check /p.yaml
```

With the binary it is `rconductor-panel profile check my-game.yaml other.yaml`. Each
problem is one line `file:line: message`, a valid file shows `file: ok`. The exit
code is 0 when all files are valid, 1 when a file has problems, and 2 for a wrong
call or a file that cannot be read. The rules are those of the import, so a file
that passes is imported.

## Command lists

Command lists live in the database and are managed under **Command lists**. An entry is
a path of whole tokens: `whitelist add` allows `whitelist add x`, but not `whitelist
remove x`, and `op` does not allow `opfoo`. Comparison follows the game profile, by
default without regard to case.

| Topic | Behaviour |
| --- | --- |
| Which list applies | the list of the moderator group through which a moderator has the server; several ways to one server add up |
| Supporters | the commands marked passable in the list of their moderator |
| Precedence | wrapper commands of the game profile are always blocked for moderators and supporters, whatever the list says |
| Catalog check | with a catalog entry, moderators and supporters are also checked against its parameters: a player must be a player name, so selectors like `@a` are refused; a choice must be listed, a number in range |
| Command separators | a profile can name them in `syntax.separators`, such as `;` on Source servers; moderators and supporters cannot send a command that holds one. Set this in your own profiles if the game's console splits commands |
| Admins | free input, no list |

## Pairing

Pairing makes panel and agent know each other's public key. There are two ways, both
for the RCON admin only.

| | With a token | By fingerprint |
| --- | --- | --- |
| Availability | always | off by default, closes itself after 30 minutes |
| Start | a token from the panel, valid once for 24 hours; it holds panel address and fingerprint | the agent connects with `RCD_PANEL_ADDR` and logs both fingerprints |
| Trust | the panel's identity is secured by the token | the admin compares fingerprints with the host operator, then approves |

## Keys and pinning

Each side has its own Ed25519 key pair, created at first start: the panel in
`/secrets/panel.key`, the agent in its `/secrets`. The connection is TLS 1.3 on port 8443,
started by the agent. Both sides pin the key of the other, so a changed key is refused
with `pin_mismatch` on both sides. A token is ignored once it has been used.

## Revocation

Revoking an agent in the panel closes its connection and detaches its servers. The agent
stays running but no longer reconnects, until it is paired again with a new token or by
fingerprint.

## Encryption

| Data | Protection |
| --- | --- |
| RCON passwords | AES-256-GCM with the master key from `/secrets/master.key` (32 random bytes) |
| User passwords | Argon2id, never reversible |
| Session IDs, tokens | only hashes are stored |
| Agent connection | TLS 1.3 with pinned keys |
| Web interface | no TLS of its own; use a reverse proxy |

A backup never contains the master key. Keep a copy of it in a safe place: if it is lost,
the panel starts, but RCON passwords have to be entered again.

Secrets never appear in logs. The one exception is the initial password of the emergency
admin, printed once at the first start.
