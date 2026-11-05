# Configuration

Only start-up and infrastructure are set by environment variables; everything
that changes during operation is a setting in the panel. All variables are
checked at start-up, and an invalid configuration stops the program with exit
code 2. Secrets also accept a `_FILE` variant that reads the value from a file,
for example a Docker secret; setting both is an error.

Booleans accept `true` and `false` (also `1` and `0`). Addresses are
`[host]:port`.

## Panel variables

| Variable | Type | Default | Allowed | Purpose |
| --- | --- | --- | --- | --- |
| `RCD_HTTP_ADDR` | address | `:8080` | `[host]:port` | web interface and API |
| `RCD_AGENT_ADDR` | address | `:8443` | `[host]:port` | agent port, TLS 1.3 with pinned keys; never behind a reverse proxy |
| `RCD_AGENT_PUBLIC_ADDR` | address | empty | `host:port` | where agents reach the panel; goes into pairing tokens |
| `RCD_PUBLIC_URL` | URL | empty | absolute `http` or `https` URL | public URL for invitation links and origin checks; with `https://` session cookies are `Secure` |
| `RCD_BASE_PATH` | path | `/` | absolute path such as `/rcon` | base path behind a reverse proxy |
| `RCD_TRUSTED_PROXIES` | list | empty | IPs or CIDR ranges, separated by commas | proxies whose `X-Forwarded-For` and `X-Forwarded-Proto` count |
| `RCD_DB_DRIVER` | text | `sqlite` | `sqlite`; `postgres` follows in release 2 | database driver |
| `RCD_DB_URL` / `RCD_DB_URL_FILE` | secret | empty | connection URL | external Postgres, release 2 |
| `RCD_DATA_DIR` | path | `/data` | directory | database and own game profiles |
| `RCD_SECRETS_DIR` | path | `/secrets` | directory | master key and panel key |
| `RCD_BACKUP_DIR` | path | `/backups` | directory | backups |
| `RCD_MASTER_KEY_FILE` | path | empty | file | read the master key from here instead of `/secrets/master.key`; it is never replaced |
| `RCD_EMERGENCY_ADMIN` | boolean | `true` | boolean | emergency admin enabled |
| `RCD_EMERGENCY_ADMIN_RESET` | boolean | `false` | boolean | on start, a new initial password for the emergency admin, its sessions end and a lock is lifted; remove it afterwards |
| `RCD_PASSWORD_HASH_CONCURRENCY` | number | `2` | 1 to 64 | password hashes computed at once, each takes 64 MiB of RAM |
| `RCD_LOG_LEVEL` | text | `info` | `debug`, `info`, `warn`, `error` | log level |
| `RCD_LOG_FORMAT` | text | `text` | `text`, `json` | log format; audit-relevant events carry a fixed event code |

## Agent variables

| Variable | Type | Default | Allowed | Purpose |
| --- | --- | --- | --- | --- |
| `RCD_PAIRING_TOKEN` / `RCD_PAIRING_TOKEN_FILE` | secret | empty | token from the panel | pairing with a token; ignored after the pairing |
| `RCD_PANEL_ADDR` | address | empty | `host:port` | panel agent port, for pairing by fingerprint or to override the token's address |
| `RCD_AGENT_NAME` | text | host name | any | name shown in the panel |
| `RCD_LABEL` | text | `rcon.manage=true` | `key=value` | label that allows a container |
| `RCD_DOCKER_HOST` | URL | `unix:///var/run/docker.sock` | Docker address, also a socket proxy such as `tcp://socket-proxy:2375` | Docker API |
| `RCD_SECRETS_DIR` | path | `/secrets` | directory | agent key and pairing |
| `RCD_LOG_LEVEL` | text | `info` | `debug`, `info`, `warn`, `error` | log level |
| `RCD_LOG_FORMAT` | text | `text` | `text`, `json` | log format |

## Settings in the panel

Only the RCON admin reads and changes settings, under **Settings**. Durations
are written like `24h`, `15m`, `30s`.

| Setting | Type | Default | Allowed | Purpose |
| --- | --- | --- | --- | --- |
| `backup.interval` | duration | 24h | 1h to 30 days | time between automatic backups |
| `backup.keep_auto` | number | 7 | 1 to 1000 | automatic backups kept |
| `backup.keep_pre_migration` | number | 3 | 1 to 1000 | backups before a migration kept |
| `session.idle_timeout` | duration | 12h | 1m to 30 days | a session ends after this time without use |
| `session.max_lifetime` | duration | 7 days | 1m to 90 days | a session ends after this time at the latest |
| `login.max_failures` | number | 5 | 1 to 100 | failed sign-ins before a lockout |
| `login.lockout_duration` | duration | 15m | 1m to 24h | length of a lockout |
| `invite.lifetime` | duration | 72h | 1m to 30 days | validity of an invitation link |
| `supporters.enabled` | boolean | off | on, off | supporters at all; switching off ends the sessions of all supporters |
| `audit.retention_days` | number | 90 | 1 to 3650 | days the audit log is kept |
| `ratelimit.rcon_admin.per_minute` | number | 120 | 1 to 10000 | commands per minute |
| `ratelimit.rcon_admin.burst` | number | 20 | 1 to 1000 | commands in one go |
| `ratelimit.server_admin.per_minute` | number | 60 | 1 to 10000 | commands per minute |
| `ratelimit.server_admin.burst` | number | 10 | 1 to 1000 | commands in one go |
| `ratelimit.moderator.per_minute` | number | 20 | 1 to 10000 | commands per minute |
| `ratelimit.moderator.burst` | number | 5 | 1 to 1000 | commands in one go |
| `ratelimit.supporter.per_minute` | number | 10 | 1 to 10000 | commands per minute |
| `ratelimit.supporter.burst` | number | 3 | 1 to 1000 | commands in one go |
| `profiles.poll_interval` | duration | 30s | 1s to 1h | how often game profile files are compared, in addition to file events |
| `ws.max_sockets_per_session` | number | 20 | 1 to 100 | WebSockets per session; beyond it the oldest closes |
| `ws.max_subscriptions_per_socket` | number | 16 | 1 to 64 | subscriptions per WebSocket |
| `agents.fingerprint_pairing` | boolean | off | on, off | pairing by fingerprint; closes itself after 30 minutes |
| `ui.shortcuts_enabled` | boolean | off | on, off | keyboard shortcuts for everyone |
