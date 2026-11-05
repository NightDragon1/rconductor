# Interface

The interface is in English and comes in a dark design (default) and a light one.
Entries you may not use are hidden, not greyed out.

## Navigation

| Entry | RCON admin | Server admin | Moderator | Supporter |
| --- | --- | --- | --- | --- |
| Overview | yes | yes | yes | yes |
| Servers | all | in scope | own | own |
| Audit | yes | in scope | – | – |
| Users | yes | moderators, supporters | supporters, if released | – |
| Groups | yes | moderator groups | – | – |
| Command lists | yes | own | – | – |
| Servers and groups | yes | read-only, in scope | – | – |
| Agents | yes | – | – | – |
| Settings | yes | – | – | – |

Your own profile is at the bottom of the side bar: real name, contacts, picture,
password.

## Overview

Cards show each server with status, load and players. The RCON admin also gets a
health overview: restart loops, disconnected agents, RCON errors and locked users,
each with a link to the object. Values that are out of date are marked, not hidden.

## Server page

The head shows status and, for admins, Start, Stop and Restart. Without an agent
only a restart over the stop command is offered. It restarts the server only if a
restart mechanism is in place, such as Docker `restart: unless-stopped`.

| Tab | For | Content |
| --- | --- | --- |
| Overview | all | status, players, and load for admins |
| Console | admins | free input, one block per command, timeline, tracks for own commands, others' commands and logs |
| Commands | moderators, supporters | allowed commands as cards with fields; player names come from the player list |
| Logs | admins | container logs over the agent, only while the view is open |
| History | all | past commands; moderators and supporters see their own |

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
