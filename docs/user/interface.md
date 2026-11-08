# Interface

The interface is in English and comes in a dark design (default) and a light one.
Entries you may not use are hidden, not greyed out.

## Navigation

| Entry | RCON admin | Server admin | Moderator | Supporter |
| --- | --- | --- | --- | --- |
| Overview | yes | yes | yes | yes |
| Servers | all | in scope | own | own |
| Topology | yes | in scope, agents without name | – | – |
| Audit | yes | in scope | – | – |
| Users | yes | moderators, supporters | supporters, if released | – |
| Groups | yes | moderator groups | – | – |
| Command lists | yes | own | – | – |
| Servers and groups | yes | read-only, in scope | – | – |
| Agents | yes | – | – | – |
| Settings | yes | – | – | – |

Your own profile is at the bottom of the side bar: real name, contacts, picture,
password, two-factor sign-in and the address for its recovery.

## Two-factor sign-in

In your profile you can switch on two-factor sign-in: confirm with your password,
scan the QR code with an authenticator app (or type in the secret) and enter the
first code. From then on signing in needs the password and a code. Switching it
off and changing the address for recovery need your password and a code.

If you lose your authenticator app, use "Lost your authenticator app?" at
sign-in: a link goes to the address in your profile, if one is on file. The link
holds once and for a short time; with your password it resets two-factor sign-in
and ends all your sessions. Without an address, whoever manages your account
resets it in the user administration. The RCON admin can require two-factor
sign-in for everyone (setting `auth.totp_required`); who has none yet must set it
up before anything else, the RCON admin too.

## Overview

Cards show each server with status, load, players, the game version and, for
admins, the game ports. The RCON admin also gets a
health overview: restart loops, disconnected agents, RCON errors and locked users,
each with a link to the object. Values that are out of date are marked, not hidden.

## Topology

The topology shows host → agent → game server, by server group, and follows
status changes live. Servers without an agent are listed on their own. The server
admin sees the agents of his servers as "Agent" with their state, without names.

## Ports and volumes

The server admin sees the game ports of his servers, the RCON admin also the RCON
port and every other published port of the container. A port that is not
published on the host is marked so. If the game profile sets `ports.steam_connect:
true`, a published game port gets a Connect link (`steam://connect/<host>:<port>`,
with the server's host). Volumes show with name and, once the agent has measured
it, their size and when it was measured.

## Server page

The head shows status and, for admins, Start, Stop and Restart. Without an agent
only a restart over the stop command is offered. It restarts the server only if a
restart mechanism is in place, such as Docker `restart: unless-stopped`.

| Tab | For | Content |
| --- | --- | --- |
| Overview | all | status, players and game version; for admins load, ports and volumes with their size and its age |
| Console | admins | free input, one block per command, timeline, tracks for own commands, others' commands and logs |
| Commands | moderators, supporters | allowed commands as cards with fields; player names come from the player list |
| Logs | admins | container logs over the agent, only while the view is open |
| History | all | past commands by role (see [roles](roles.md#who-sees-what)); a switch shows only your own |

In the console, Tab takes the first suggestion and the arrow keys walk through your
own commands.

## Administration

Lists can be searched. The user list also has filters and multiple selection with
mass actions. Details open in a panel at the side, the list stays visible.

Changes wait five seconds, so you can undo them; leaving the page sends them at once.
Deleting, revoking, a change of role and switching supporters off ask first. If
someone else changed the object in the meantime, you see "Changed by someone else –
reload" and nothing is overwritten.

Keyboard shortcuts are off unless the RCON admin switches them on.

## On a phone

Cards, the command page and the console work fully. The side bar becomes a menu and
server actions move to a menu in the head. The administration only reads, with three
exceptions for emergencies: lock, unlock and ending sessions, each with a dialog.

## Text from game servers

Everything from game servers and agents is shown as plain text. Color codes are
removed. Characters that reverse the reading direction show as a stand-in such as
⟨U+202E⟩; copying gives the original text.

Over plain http the browser offers no clipboard: the copy buttons select the text and
ask for Ctrl+C. HTTPS through a reverse proxy gives direct copying.
