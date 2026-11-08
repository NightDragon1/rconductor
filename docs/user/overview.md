# Overview

RCONductor is a self-hosted web panel for game servers that speak RCON. It
gives a team one place to run commands, with exact rights per person: an admin
has a free console, a moderator only the commands of a list, as buttons with
fields. Everything is recorded in an audit log.

It is built for your own LAN or VPN, not for the open internet.

## Panel and agent

| Part | Runs | Does |
| --- | --- | --- |
| Panel | once | web interface, API, rights, RCON connections, audit, database |
| Agent | on each game host, optional | status, metrics and logs of containers; start, stop and restart |

The panel talks RCON to the game servers directly. That is enough for commands,
player lists and server status. Without an agent you still have all of this;
what you lose is everything that needs the container: load, logs, start and
stop.

## Why there is an agent

Access to the Docker socket is equal to root on the host. So the panel, the part
that faces your users, never has it. A small agent on each game host holds that
access instead, and it does little with it:

- It opens no port. It connects out to the panel.
- It accepts only a fixed list of orders, and only for containers with the label
  `rconductor.manage=true`.
- It cannot create, remove or change containers, run commands in them or touch
  images and volumes.
- Panel and agent know each other by pinned keys, not by passwords.

An attacker who gets into the panel can therefore not simply take over your game
hosts. A socket proxy in front of the socket narrows the agent further
([Installation](installation.md#agent-behind-a-socket-proxy)).

## What comes next

Messages, German as a second language, Postgres and more protocols are planned
for later releases.
