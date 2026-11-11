# Upgrade test

A synthetic update of the panel and the agent from one release to another
(NFR-013, SRV-008, AGT-013), with Docker on the local machine:

```sh
test/upgrade/run.sh OLD_PANEL_IMAGE OLD_AGENT_IMAGE NEW_PANEL_IMAGE NEW_AGENT_IMAGE

# for example, 1.0 to images built from this checkout
make images PANEL_IMAGE=rcdupd/panel:main AGENT_IMAGE=rcdupd/agent:main
test/upgrade/run.sh docker.io/nightdragon1/rconductor-panel:1.0 \
  docker.io/nightdragon1/rconductor-agent:1.0 rcdupd/panel:main rcdupd/agent:main
```

Images that are not there are pulled. The script needs `docker` with Compose,
`curl`, `jq` and `go` (it builds the two helpers below); nothing is installed.

## What it does

1. Starts the old panel, the old agent and a stand-in game server
   (`rconsrv`, the RCON test server of `internal/rcon/rcontest`) in a Compose
   project of its own with its own volumes; the web port is bound to
   127.0.0.1 only.
2. Fills the old panel through its API: the emergency admin with the initial
   password from the log, a server admin, two moderators (one locked) and a
   supporter by invitation, servers with different profiles and one that is
   unreachable, server groups, scopes, a command list, a moderator group, a
   general group, supporter grants, favorites, a paired agent linked to a
   server, commands of every role (allowed and refused), and one open session
   per role.
3. Takes a snapshot through the API, stops the old panel, counts the rows of
   every table (`dbstat` in a container on the data volume) and removes the
   profile of one server in the database, as older databases can hold it.
4. Starts the new panel on the same volumes, with the old agent still running,
   and checks: migration log lines and schema version, the backup before the
   migration, sessions of the old panel, password login of every role, users,
   scopes, servers and their profile lists, groups, command lists, favorites,
   settings, history with author and role, audit log, commands of every role,
   the old agent connected with its container data and without the functions
   it lacks, and no errors in the logs.
5. Updates the agent to the new image and checks that it reconnects with its
   pairing.
6. Stops the panel and compares the row counts of every table.

It ends with a table `status | check | expected | result` and exits non-zero
if a check fails.

## Settings

| Variable | Default | Purpose |
| --- | --- | --- |
| `RCD_UPGRADE_PROJECT` | `rcdupgrade<pid>` | Compose project name; volumes and containers carry it |
| `RCD_UPGRADE_PORT` | first free port from 18180 | web port of the panel on 127.0.0.1 |
| `RCD_UPGRADE_SCHEMA` | highest migration number in this checkout | schema version the new panel must reach |
| `RCD_UPGRADE_DOCKER_GID` | group of `/var/run/docker.sock` | group for the agent |
| `RCD_UPGRADE_WAIT` | `90` | seconds to wait for panel, agent and RCON |
| `RCD_UPGRADE_KEEP` | `0` | `1` keeps containers, volumes and the work directory for debugging |

## Secrets and clean-up

Every password and token is generated at run time and kept in a private
temporary directory (`TMPDIR`), never in the repository. Output is masked; the
initial password is read from the log and never printed. On exit, also after a
failure or Ctrl-C, the script removes its Compose project with its volumes and
the temporary directory, and nothing else. Images are left alone.
