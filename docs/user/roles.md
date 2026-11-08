# Roles and rights

There are four fixed roles in a strict order. Rights only flow downwards, and
the right to manage others is never passed on. The backend checks every request;
the interface only hides what a role may not use.

| Role | Servers | Commands | Agent functions | Manages |
| --- | --- | --- | --- | --- |
| RCON admin | all | free, unfiltered | all, everywhere | everything: servers, server groups, agents, server admins, the supporter switch |
| Server admin | assigned server groups and single servers | free, unfiltered | all, within scope | moderators, moderator groups, command lists, passable commands |
| Moderator | assigned servers or server groups, within the scope of the server admin | only the command list of the moderator group | none | supporters per server, if released |
| Supporter | assigned servers of the moderator | only the commands the list marks as passable | none | nothing |

## How rights are assigned

The chain is RCON admin → server admin → moderator → supporter.

1. The RCON admin assigns server groups or single servers to a server admin.
2. The server admin creates a command list and a moderator group, links the two
   and gives the group servers.
3. A moderator with the supporter release assigns supporters to servers. This
   needs the global setting `supporters.enabled` too.

- Assignments are always a subset of your own scope and commands.
- You manage only users of a lower role within your scope.
- When a scope shrinks at the top, it shrinks at once for everyone below.
- One user can hold several assignments.
- In a command list at least one command stays locked for supporters.

## What ends a session

Locking or removing a user, and every change of role, scope, group membership,
grants or supporter release, ends that user's sessions at once, open WebSockets
included. A role change also removes the assignments of the old role. Changes to
the content of a command list work from the next request, without a new sign-in.

## Who sees what

Real name, picture and contact details are visible to the user, the RCON admin
and whoever manages that user. Everyone else sees only the pseudonym.

Moderators and supporters never see agent data such as load or containers.

Commands in the history and in the live console are visible by role, on the
servers a user may reach:

| Viewer | Sees the commands of |
| --- | --- |
| RCON admin | everyone, on all servers |
| Server admin | server admins, moderators and supporters |
| Moderator, supporter | moderators and supporters |

The role at the time of the command counts, also when the user changed role or
was deleted later. Every role can narrow the view to its own commands.

## The emergency admin

The emergency admin `admin` is a local RCON admin that always works. If its
password is lost, start the panel once with `RCD_EMERGENCY_ADMIN_RESET=true`.
