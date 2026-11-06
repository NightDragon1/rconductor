# Contributing

Thank you for your interest in RCONductor. The project follows an approved
concept in `docs/` (German), with a requirement ID for each feature. Please
open an issue before larger work, so we can agree that it fits the concept
and the release plan. Report vulnerabilities as described in
[SECURITY.md](SECURITY.md), not in an issue.

## Setup

You need Go, Node.js and Docker in the versions from `go.mod`, the workflows
and `web/package.json`. Enable the pre-commit hook once per clone:

```sh
git config core.hooksPath .githooks
```

## Checks

Run this before every commit; the hook does it too:

```sh
make check
```

It runs the linters, formatting, the check of generated code and all tests,
and must pass. Stage generated files with the change: they count as part of
it. Further tests are `make test-integration` and `make test-e2e`; the CI
runs them on pull requests, E2E when the changed paths concern it.
`make test-minecraft` runs nightly and before a release; locally it needs you
to accept the Minecraft EULA yourself (see the README).

`api/openapi.yaml` is the only source of the API: change it first, then
generate the code.

## Commits

Use small commits with an English message in this form, naming the
requirement IDs from `docs/01-gesamtkonzept.md`:

```text
feat(cmd): token path check (CMD-004)
fix(web): long server names wrap on a phone (UI-012, SEC-006)
```

The types are `feat`, `fix`, `test`, `docs`, `ci` and `chore`. A requirement
is done when its acceptance criterion in `docs/05-abnahme-r1.md` passes in a
test. Please do not change the concept in `docs/`; propose changes in an
issue.

## Merging

Pull requests are merged after a green CI, by Claude Code as well. Changes to
`CLAUDE.md`, `CONTRIBUTING.md`, the release workflow with its scripts and the
rulesets are merged by the owner only.

## Release

- A release starts only with a tag `v*`, such as `v1.0`, and needs the approval in
  the environment `release`.
- The Docker Hub secrets live only in that environment.
- The release notes come from `CHANGELOG.md`, which lists real releases only.
- Test versions `0.x` follow D-047: only the image tag `0.x`, no major or `latest`
  tag, a GitHub pre-release with the fixed text "Testversion vor 1.0" and its known
  issues from `scripts/release-notes.sh`, and no entry in the changelog. Signature and SBOM are the same as for a real release.

## License headers

Every source file starts with an SPDX line in its first lines, in the comment
syntax of the file:

```go
// SPDX-License-Identifier: AGPL-3.0-or-later
```

`scripts/check-spdx.sh` checks this. Exceptions:

- sqlc query files in `internal/store/queries/` carry the line at the end
- Markdown, JSON, `docs/` and generated code need none
- copied third-party code keeps its license and is listed in
  `THIRD_PARTY_NOTICES.md`

New dependencies must be compatible with the AGPL; name the reason in the
pull request. By contributing you agree that your work is licensed under
AGPL-3.0-or-later.
