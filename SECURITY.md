# Security policy

RCONductor is under development; there is no released version yet. Reports
apply to the current state of `main`.

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub:
[Report a vulnerability](https://github.com/NightDragon1/rconductor/security/advisories/new)
(Private Vulnerability Reporting). Do not open a public issue, pull request
or discussion for a vulnerability.

Please include:

- the affected component (panel, agent, web interface) and the commit
- how to reproduce it, and what an attacker gains
- a proposed fix, if you have one

This is a project run in spare time, so there are no fixed response times.
You will get an answer in the advisory, and credit in it if you wish.

## Scope

In scope is the code in this repository, for example rights checks, the
agent protocol, handling of data from game servers and agents, and secrets
in logs.

Out of scope are the game servers themselves and vulnerabilities in a
deployment that follow from a configuration the documentation warns about.
