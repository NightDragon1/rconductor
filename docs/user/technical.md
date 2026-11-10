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
| Kind | `kind: generic` for a game as such, `kind: specific` for a modpack or plugin that ranks above it; a profile without `kind` is generic. Profile files inherit nothing from each other; a server combines the profiles in its list (1.1) |
| Origin | `researched_version` names the game version the commands were researched for, `sources` lists where they come from, `status` is `tested` after a run with a real server and `untested` otherwise; a profile without `status` is untested (1.1) |
| Errors | an invalid file is not imported; the last valid state stays active, the error is in the log and the health overview |
| Aliases | a file has at most 100 YAML aliases |
| Anchors | an anchored value must not contain aliases |
| Nesting | lists and mappings nest at most 64 levels deep |
| Empty values | a field is written with a value or left out; `null` and a key without a value are refused, write `[]` or `""` for an empty value |

### Shipped profiles

The panel ships these profiles (1.1). A `specific` profile, such as a modpack or a
plugin, adds to a `generic` one; give the server both, the specific one on top (see
[several profiles per server](#several-profiles-per-server)).

| Id | Name | Kind |
| --- | --- | --- |
| `ark-sa` | ARK: Survival Ascended | generic |
| `ark-se` | ARK: Survival Evolved | generic |
| `conan-exiles` | Conan Exiles | generic |
| `factorio` | Factorio | generic |
| `insurgency-sandstorm` | Insurgency: Sandstorm | generic |
| `minecraft-create` | Create | specific |
| `minecraft-essentialsx` | EssentialsX | specific |
| `minecraft-forge` | Minecraft Forge | generic |
| `minecraft-ftb-chunks` | FTB Chunks | specific |
| `minecraft-ftb-essentials` | FTB Essentials | specific |
| `minecraft-ftb-quests` | FTB Quests | specific |
| `minecraft-ftb-ranks` | FTB Ranks | specific |
| `minecraft-ftb-teams` | FTB Teams | specific |
| `minecraft-gtnh` | Minecraft GT New Horizons (1.7.10) | specific |
| `minecraft-java-legacy` | Minecraft (Java Edition 1.12.2) | generic |
| `minecraft-java` | Minecraft (Java Edition) | generic |
| `minecraft-luckperms` | LuckPerms | specific |
| `minecraft-neoforge` | Minecraft NeoForge | generic |
| `minecraft-paper` | Minecraft Paper (Spigot/Bukkit) | generic |
| `minecraft-pixelmon` | Pixelmon Reforged | specific |
| `minecraft-spark` | spark (profiler) | specific |
| `mordhau` | Mordhau | generic |
| `palworld` | Palworld | generic |
| `project-zomboid` | Project Zomboid | generic |
| `satisfactory` | Satisfactory | generic |
| `source-cs2` | Counter-Strike 2 | specific |
| `source-engine` | Source Engine (srcds) | generic |
| `source-gmod` | Garry's Mod | specific |
| `source-l4d2` | Left 4 Dead 2 | specific |
| `squad` | Squad | generic |
| `starbound` | Starbound | generic |
| `v-rising` | V Rising | generic |

### Several profiles per server

A server has an ordered list of game profiles, top first (1.1). For each value the
topmost profile that sets it wins; a value that no profile sets keeps its default.
This holds field by field. Example: a modpack profile (specific) without its own
prefix rule above a Minecraft profile (generic): the commands of both apply, and the
prefix rule comes from Minecraft.

| Topic | Behaviour |
| --- | --- |
| Values | field by field from the topmost profile that sets them: syntax (case sensitivity, prefix removal, maximum length, player pattern, separators), game ports, Steam connect, players block, stop command, version pattern, formatting |
| Command catalog | the union of all profiles; with the same path the entry of the upper profile wins |
| Wrappers | the wrappers of all profiles hold together; no profile lifts one |
| Protocol | all profiles of a server have the same protocol, for `http-api` also the same dialect; otherwise saving is refused (422) |
| Adding | a specific profile goes above the topmost generic one, a generic one goes to the end; the interface marks a generic profile above a specific one |
| Who changes it | the RCON admin; the server admin on servers in his scope, but only the profile list (other server fields give 403 and change nothing) |
| API | the server has `profile_ids`, top first; the former `profile_id` is gone, so scripts that used it must change |
| Update from 1.0 | the database migration moves the one profile of each server to the top of the list; nothing to do |

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
remove x`, and `op` does not allow `opfoo`. Comparison follows the game profiles of the
server, by default without regard to case.

| Topic | Behaviour |
| --- | --- |
| Which list applies | the list of the moderator group through which a moderator has the server; several ways to one server add up |
| Supporters | the commands marked passable in the list of their moderator |
| Precedence | wrapper commands of the server's game profiles are always blocked for moderators and supporters, whatever the list says |
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

## Certificates of game servers with an HTTPS API

A game server with an HTTPS API (protocol `http-api`, such as Satisfactory) usually has a
self-signed certificate. The RCON admin trusts it by its SHA-256 fingerprint, as the
connection test shows it; until then, and whenever the server shows another certificate
(`cert_mismatch`), the API token never leaves the panel.

| Topic | Behaviour |
| --- | --- |
| Expiry | the trust is the fingerprint only and holds after the certificate's end of validity; the panel stores that date when the certificate is trusted, or at the first connection with it, and shows it |
| Renewal | when the server makes a new certificate (for example after its settings were reset or the old one ran out), the fingerprint changes and the server shows `cert_mismatch` |
| Trusting again | run the connection test, compare the new fingerprint with the server, then trust it; the stored date is cleared with the old fingerprint |

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
