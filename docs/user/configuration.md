# Configuration

Applies to version 1.0 and to main (1.1). Entries marked (1.1) are new in
1.1; version 1.0 does not have them.

Only start-up and infrastructure are set by environment variables; everything
that changes during operation is a setting in the panel. All variables are
checked at start-up, and an invalid configuration stops the program with exit
code 2. Secrets also accept a `_FILE` variant that reads the value from a file,
for example a Docker secret; setting both is an error.

Booleans accept `true` and `false` (also `1` and `0`). Addresses are
`[host]:port`.

## Compose files

These only fill in the Compose files in `deploy/compose/`; the programs do not read them.

| Variable | Type | Default | Allowed | Purpose |
| --- | --- | --- | --- | --- |
| `RCD_VERSION` | text | `1` | an image tag such as `1` or `1.0` | version of panel and agent images |
| `RCD_IMAGE_REPO` | text | `docker.io/nightdragon1` | also `ghcr.io/nightdragon1` | registry and owner of the images in the Compose files |
| `RCD_WEB_PORT` | number | `8080` | a free port | published web port |
| `RCD_AGENT_PORT` | number | `8443` | a free port | published agent port (panel.yaml) |
| `DOCKER_GID` | number | `999` | group of `/var/run/docker.sock` | lets the agent reach the socket |

## Panel variables

| Variable | Type | Default | Allowed | Purpose |
| --- | --- | --- | --- | --- |
| `RCD_HTTP_ADDR` | address | `:8080` | `[host]:port` | web interface and API |
| `RCD_AGENT_ADDR` | address | `:8443` | `[host]:port` | agent port, TLS 1.3 with pinned keys; never behind a reverse proxy |
| `RCD_IMAGE_REPO` | text | `docker.io/nightdragon1` | registry and owner | the image the pairing dialog names; the Compose files pass it on |
| `RCD_AGENT_PUBLIC_ADDR` | address | empty | `host:port` | where agents reach the panel; goes into pairing tokens |
| `RCD_PUBLIC_URL` | URL | empty | absolute `http` or `https` URL | public URL for invitation links and origin checks; with `https://` session cookies are `Secure`; (1.1) recovery mails of the second factor go out only with it set |
| `RCD_BASE_PATH` | path | `/` | absolute path such as `/rcon` | base path behind a reverse proxy |
| `RCD_TRUSTED_PROXIES` | list | empty | IPs or CIDR ranges, separated by commas | proxies whose `X-Forwarded-For` and `X-Forwarded-Proto` count |
| `RCD_DB_DRIVER` | text | `sqlite` | `sqlite`; `postgres` is not supported yet | database driver |
| `RCD_DB_URL` / `RCD_DB_URL_FILE` | secret | empty | connection URL | external Postgres, not supported yet |
| `RCD_DATA_DIR` | path | `/data` | directory | database and own game profiles |
| `RCD_SECRETS_DIR` | path | `/secrets` | directory | master key and panel key |
| `RCD_BACKUP_DIR` | path | `/backups` | directory | backups |
| `RCD_MASTER_KEY_FILE` | path | empty | file | read the master key from here instead of `/secrets/master.key`; it is never replaced |
| `RCD_EMERGENCY_ADMIN` | boolean | `true` | boolean | emergency admin enabled |
| `RCD_EMERGENCY_ADMIN_RESET` | boolean | `false` | boolean | on start, a new initial password for the emergency admin, its sessions end and a lock is lifted; remove it afterwards |
| `RCD_PASSWORD_HASH_CONCURRENCY` | number | `2` | 1 to 2 | password hashes computed at once, each takes 64 MiB of RAM |
| `RCD_MAX_CONNECTIONS` | number | `512` | 16 to 65536 | open connections of the web port, WebSockets included; further ones wait until one ends. A connection can hold up to 256 KiB of a request body, so 512 take up to 128 MiB: raise `mem_limit` of the panel along with this value |
| `RCD_MAX_CLIENT_CONNECTIONS` | number | `128` | 1 to 65536 | open connections of one client address, an IPv6 /64 counts as one; further ones are closed; trusted proxies have no limit of their own |
| `RCD_MAX_ANONYMOUS_CONNECTION_SECONDS` | number | `30` | 10 to 3600 | a connection without a valid session ends after this time, once its running request is done; behind a trusted proxy the proxy address is exempt |
| `RCD_RCON_PROBE_SECONDS` | number | 2 | 1 to 60 | how often an idle RCON connection is read |
| `RCD_RCON_IDLE_PROBE_SECONDS` | number | 30 | 5 to 600 | how often an idle RCON connection gets a probe |
| `RCD_RCON_PROBE_TIMEOUT_SECONDS` | number | 5 | 1 to 60 | answer time of the probe |
| `RCD_RCON_MAX_BACKOFF_SECONDS` | number | 5 | 1 to 120 | longest wait between two RCON connection attempts |
| `RCD_RCON_DIAL_TIMEOUT_SECONDS` | number | 5 | 1 to 60 | time for one RCON connection attempt |
| `RCD_RCON_QUEUE` | number | 64 | 1 to 1024 | commands waiting per game server |
| `RCD_COMMAND_TIMEOUT_SECONDS` | number | 10 | 1 to 120 | time for one command, waiting included |
| `RCD_PLAYERS_REFRESH_SECONDS` | number | 60 | 10 to 3600 | how often a viewed player list is fetched |
| `RCD_AGENT_MAX_CONNECTIONS` | number | 1024 | 16 to 65536 | open connections at the agent port |
| `RCD_AGENT_MAX_CLIENT_CONNECTIONS` | number | 64 | 1 to 65536 | of those per client address |
| `RCD_AGENT_CONNECTS_PER_MINUTE` | number | 10 | 1 to 600 | new connections per address and minute at the agent port |
| `RCD_AGENT_CONNECT_BURST` | number | 5 | 1 to 100 | of those in one go |
| `GOMEMLIMIT` | text | empty | as in Go, such as `800MiB`, or `off` | memory the Go runtime aims for; empty means 80 % of the memory limit of the container, or none without one |
| `RCD_LOG_LEVEL` | text | `info` | `debug`, `info`, `warn`, `error` | log level |
| `RCD_LOG_FORMAT` | text | `text` | `text`, `json` | log format; audit-relevant events carry a fixed event code |

## Agent variables

| Variable | Type | Default | Allowed | Purpose |
| --- | --- | --- | --- | --- |
| `RCD_PAIRING_TOKEN` / `RCD_PAIRING_TOKEN_FILE` | secret | empty | token from the panel | pairing with a token; ignored after the pairing |
| `RCD_PANEL_ADDR` | address | empty | `host:port` | panel agent port, for pairing by fingerprint or to override the token's address |
| `RCD_AGENT_NAME` | text | host name | any | name shown in the panel |
| `RCD_LABEL` | text | `rconductor.manage=true` | `key=value` | label that allows a container |
| `RCD_AGENT_MAX_BACKOFF_SECONDS` | number | 120 | 5 to 3600 | longest wait between two connection attempts |
| `RCD_AGENT_DIAL_TIMEOUT_SECONDS` | number | 15 | 1 to 120 | time for one connection attempt |
| `RCD_METRICS_INTERVAL_SECONDS` | number | 10 | 1 to 300 | how often metrics are sent |
| `RCD_MAX_LOG_FOLLOWS` | number | 16 | 1 to 256 | live logs at once |
| `RCD_ACTIONS_PER_MINUTE` | number | 10 | 1 to 600 | start, stop, restart per container and minute |
| `RCD_ACTIONS_BURST` | number | 3 | 1 to 100 | of those in one go |
| `RCD_FOLLOWS_PER_MINUTE` | number | 60 | 1 to 600 | starts of live logs per container and minute |
| `RCD_FOLLOWS_BURST` | number | 20 | 1 to 100 | of those in one go |
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
| `login.max_client_failures` | number | 50 | 1 to 1000 | failed sign-ins from one address over all names within the lockout duration before that address is locked; everyone behind one address shares this count, so raise it for a large shared address |
| `login.overflow_gap` | duration | 250ms | 50ms to 10s | time between two sign-ins in the waiting line when the table is full |
| `login.overflow_line` | number | 30 | 1 to 1000 | places in that waiting line |
| `login.max_tracked` | number | 100000 | 1000 to 1000000 | login counters kept in memory; on a full table sign-ins not yet counted wait in one line |
| `login.lockout_duration` | duration | 15m | 1m to 24h | length of a lockout |
| `invite.lifetime` | duration | 72h | 1m to 30 days | validity of an invitation link |
| `supporters.enabled` | boolean | off | on, off | supporters at all; switching off ends the sessions of all supporters |
| `audit.retention_days` | number | 90 | 30 to 3650 | days the audit log is kept |
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
| `servers.connection_test_per_minute` | number | 10 | 1 to 120 | connection tests per user and minute |
| `commands.templates_per_server` (1.1) | number | 100 | 1 to 1000 | command templates per server |
| `commands.multi_max_servers` (1.1) | number | 50 | 1 to 1000 | servers of one command to several servers |
| `commands.multi_parallel` (1.1) | number | 8 | 1 to 64 | servers a command to several servers runs on at the same time |
| `agents.fingerprint_pairing` | boolean | off | on, off | pairing by fingerprint; closes itself after 30 minutes |
| `agent.version_retry` (1.1) | duration | 30s | 5s to 1h | how often the panel asks the agent again for the game version; each attempt reads only the log since the last one |
| `agent.version_window` (1.1) | duration | 10m | 1m to 24h | how long the panel looks for the game version after it first sees a container run |
| `agent.volume_interval` (1.1) | duration | 2h | 10m to 7 days | how often each agent measures the used size of its volumes, in one call for all containers |
| `agent.volume_stale` (1.1) | duration | 6h | 30m to 30 days | after this a measured volume size counts as old |
| `auth.totp_required` (1.1) | boolean | off | on, off | two-factor sign-in for everyone; who has none yet must set it up before anything else |
| `auth.second_factor_window` (1.1) | duration | 5m | 1m to 1h | time for the code after the right password; 5 wrong codes end it too |
| `auth.recovery_lifetime` (1.1) | duration | 30m | 5m to 24h | validity of a recovery link for the second factor; only the last one requested holds |
| `auth.recovery_per_hour` (1.1) | number | 3 | 1 to 100 | recovery requests per user and per client address and hour |
| `smtp.timeout` (1.1) | duration | 10s | 1s to 5m | longest time for one mail, from connecting to the end |
| `smtp.test_per_minute` (1.1) | number | 5 | 1 to 60 | test mails per user and minute |
| `ui.shortcuts_enabled` | boolean | off | on, off | keyboard shortcuts for everyone |

## Mail (SMTP) (1.1)

The RCON admin sets up the SMTP server under Settings → Mail (SMTP). The panel uses
it for its mail, first to recover a second factor. Every change is in the audit,
without the password.

| Field | Type | Default | Allowed | Purpose |
| --- | --- | --- | --- | --- |
| SMTP host | host name or IP address | empty | up to 253 characters, no port | the SMTP server |
| Port | number | 587 | 1 to 65535 | its port, typically 587 for STARTTLS and 465 for SSL/TLS |
| Encryption | choice | STARTTLS | STARTTLS, SSL/TLS, none | with STARTTLS and SSL/TLS the certificate of the server is always checked; STARTTLS is a must once chosen, the panel never falls back to plain text |
| User, Password | text | empty | up to 256 and 512 characters | sign-in at the SMTP server; only with STARTTLS or SSL/TLS. The password is stored encrypted with the master key and never shown again; a new host or port keeps it |
| Sender address | mail address | empty | one plain address | the From of the panel's mail |

"Send a test mail" sends a fixed mail to an address you type in. A failure names
only its kind: `auth` (user or password refused), `tls` (STARTTLS missing or the
certificate not trusted), `connect`, `timeout` or `rejected` (sender or recipient
refused). The log notes the same with the codes `mail.sent` and `mail.failed`,
with the recipient's domain only.
