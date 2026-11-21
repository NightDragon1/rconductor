# Interface

> **Version:** applies to RCONductor 1.2. Parts marked (new in 1.2)
> are not in 1.1, parts marked (new in 1.1) are not in 1.0.

The interface is in English and comes in a dark design (default) and a light one.
Entries you may not use are hidden, not greyed out. (new in 1.2) Below the heading
of every page one grey line says what the page is for.

The words keep one meaning: a host is a machine, a server is a game server, a server
group is a group of game servers, and a moderator group is a group of users with a
command list.

## Navigation

| Entry | RCON admin | Server admin | Moderator | Supporter |
| --- | --- | --- | --- | --- |
| Overview | yes | yes | yes | yes |
| Servers, tab Cards | all | in scope | own | own |
| Servers, tab Manage servers and server groups (new in 1.2) | yes | read-only, in scope; game profiles of a server | – | – |
| Topology (new in 1.1) | yes | in scope, hosts without agent details | – | – |
| Audit | yes | in scope | – | – |
| Game profiles (new in 1.2) | yes | yes | yes | yes |
| Messages (new in 1.2) | yes | yes | yes | yes |
| Users | yes | moderators, supporters | supporters, if released | – |
| Moderator groups | yes | yes | – | – |
| Command lists | yes | own | – | – |
| Agents | yes | – | – | – |
| Settings | yes | – | – | – |
| System log (new in 1.2) | yes | – | – | – |

Your own profile is at the bottom of the side bar: real name, contacts, picture,
password, and (new in 1.1) two-factor sign-in and the address for its recovery.
(new in 1.2) About, also in the side bar, has two tabs: About with version, commit,
build date and the paired agents, and Third-party with the full list of the Go and
UI components and their licenses.

## Two-factor sign-in (new in 1.1)

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

Cards show each server with status, load and players, (new in 1.1) the game version
and, for admins, the game ports. The RCON admin also gets a
health overview: restart loops, disconnected agents, RCON errors and locked users,
each with a link to the object. Values that are out of date are marked, not hidden.
(new in 1.2) If a container has a healthcheck, its card shows healthy, starting or
unhealthy instead of running; paused, restarting and dead are shown by name.

## Servers (new in 1.2)

Servers has two tabs. Cards shows every server you may see as a card, with a search.
Manage servers and server groups, for the RCON admin and the server admin, is where
servers are created and edited and server groups are formed; the server admin only
reads it, apart from the game profiles of a server. It replaces the entry Servers and
groups under Administration; its old address leads to the tab.

## Topology (new in 1.1)

(new in 1.2) The topology is a tree from left to right: the panel with its version,
then each host with its agent, then the game servers on that host. It follows status
changes live. Servers without an agent hang on a branch of their own. A host shows
its display name, or "Host 1", "Host 2" and so on without one. The RCON admin also
sees agent name, address and version, and a host opens its agent; an agent whose
major version differs from the panel is marked. The server admin sees the hosts of
his servers with their state, without agent details.

## Ports and volumes (new in 1.1)

The server admin sees the game ports of his servers, the RCON admin also the RCON
port and every other published port of the container. A port that is not
published on the host is marked so. If the game profile sets `ports.steam_connect:
true`, a published game port gets a Connect link (`steam://connect/<host>:<port>`,
with the server's host); as it shows the host, only the RCON admin sees it.
Volumes show with name and, once the agent has measured
it, their size and when it was measured.

## Server page

The head shows status and, for admins, Start, Stop and Restart. Without an agent
only a restart over the stop command is offered. It restarts the server only if a
restart mechanism is in place, such as Docker `restart: unless-stopped`.

| Tab | For | Content |
| --- | --- | --- |
| Overview | all | status and players, for admins load; (new in 1.1) game version, for admins ports and volumes with their size and its age |
| Console | admins | free input, one block per command, timeline, tracks for own commands, others' commands and logs; (new in 1.1) templates, one command to several servers |
| Commands | moderators, supporters | allowed commands and (new in 1.1) templates as cards with fields; player names come from the player list |
| Logs | admins | container logs over the agent, only while the view is open |
| History | all | past commands by role (see [roles](roles.md#who-sees-what)); (new in 1.1) moderators and supporters also see those of the others on their servers, a switch shows only your own |

In the console, Tab takes the first suggestion and the arrow keys walk through your
own commands. When every command of the server starts with `/`, as in Factorio, input
without the `/` shows a hint before you send it: such games may take it as chat and
not answer. The console leaves the text as you typed it.

### Several servers (new in 1.1)

Under Several servers in the console the RCON admin and the server admin send one
command to several servers of their scope, for example a notice before maintenance.
Each server checks the command on its own, with the syntax of its game profile and
its own rate limit, as if it were typed there; the results come per server. A server
outside the scope shows as not found. Up to `commands.multi_max_servers` servers go
in one call.

### Templates (new in 1.1)

A template is a command with placeholders, such as `kick {player} {reason}`. The
RCON admin and the server admin keep the templates of a server under Templates in
the console: new, edit, delete. Placeholders are `{name}` in lower case; `{player}`
picks from the player list; the first word is fixed text. A name is used once per
server. A filled template goes to the input line.

Moderators and supporters see the templates whose fixed part, the words before the
first placeholder, is a command of their list, by whole words: `kick` allows
`kick {player}`, not `kickall {player}`. A filled template is checked like a typed
command, so a value the list or the game profile does not allow is refused.

## Messages (new in 1.2)

Messages lists your messages, newest first, with unread ones marked; a message opens
as a conversation with its comments, where you can reply. The RCON admin writes to
all users, the server admins, the moderators or chosen users; the server admin to
everyone on his servers or to their moderators. Deleting a message removes it and its
comments for every recipient.

## Administration

Lists can be searched. (new in 1.2) From 1024 px wide they are dense tables. The user list also has filters and multiple selection with
mass actions. Details open in a panel at the side, the list stays visible.
(new in 1.2) Open invitations stand in the user list too; whoever made one, and the
RCON admin, can withdraw it.

(new in 1.2) Settings are in groups such as Security, Users, Servers and commands,
Agents, System and Features. From 1024 px wide one group shows at a time, picked from
the list at the side; below that all groups follow each other. Values that are
rarely needed sit folded under Advanced in their section.

Changes wait five seconds, so you can undo them; leaving the page sends them at once.
Deleting, revoking, a change of role and switching supporters off ask first. If
someone else changed the object in the meantime, you see "Changed by someone else –
reload" and nothing is overwritten.

Keyboard shortcuts are off unless the RCON admin switches them on.

The new functions of 1.2 are on. Under Settings, Features, the RCON admin can
switch each of them off; a switched-off function is gone for every role.

### Game profiles of a server

In the server form, Game profiles is a list, top first. **+** adds a profile, **−**
removes one. Drag a profile, or use its move buttons, to change the order. A new
specific profile goes above the topmost generic one, a new generic one to the end.
If a generic profile stands above a specific one, a hint says so; you can still
save. All profiles need the same protocol, otherwise saving is refused. The server
admin can change this list on the servers of his scope, but no other field of the
server. How the profiles combine is in the [technical
notes](technical.md#several-profiles-per-server).

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
