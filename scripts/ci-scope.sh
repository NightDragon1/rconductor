#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Decides what a CI run does and prints it as GITHUB_OUTPUT lines:
#   stage2     push to main, pull requests, nightly, tags: integration, E2E
#              and the amd64 image
#   stage3     nightly, manual and tags: arm64 image and Minecraft
#   e2e        E2E is due, by the changed paths
#   minecraft  Minecraft is due, by the changed paths
#   sqlc       the sqlc diff is due, by the changed paths
#   code       anything beyond documentation changed: build, lint, tests,
#              govulncheck and images are due (PRJ-005); with documentation
#              alone only Scope, SPDX and the collecting jobs run
# Nightly, manual and tag runs do everything. Without a usable base commit
# (first push of a branch, force push) all paths count as changed.
# Every changed path counts as it is: names come NUL-separated and
# unquoted, a moved file with its old and its new path. A path outside the
# known top-level entries, or a diff that fails, turns every stage on.
# Needs EVENT, REF and BASE from the workflow.
set -eu
export LC_ALL=C
E2E='^(web/|api/|internal/|cmd/|migrations/|profiles/|go\.(mod|sum)$|Makefile$|\.github/workflows/|scripts/ci-scope\.sh$)'
MINECRAFT='^(internal/(rcon|connmgr)/|internal/app/minecraft_test\.go$|profiles/|scripts/minecraft|\.github/workflows/|scripts/ci-scope\.sh$)'
SQLC='^(migrations/|internal/store/queries/|internal/store/sqlitedb/|sqlc\.yaml$)'
# Documentation: no build, test or image is due for it. Files that tests
# read stay code (README.md: Go and release script tests; the user guide
# pages checked against the code).
DOCS='(\.md$|^docs/|^assets/|^LICENSE|^\.github/ISSUE_TEMPLATE/)'
DOCTESTED='^(README\.md|docs/user/configuration\.md|docs/user/technical\.md)$'
# Paths that need none of these stages.
NONE='^(\.claude/|\.github/|\.githooks/|deploy/|docs/|scripts/|tools/|\.dockerignore$|\.editorconfig$|\.gitattributes$|\.gitignore$|\.golangci\.yml$|CHANGELOG\.md$|CLAUDE\.md$|CONTRIBUTING\.md$|Dockerfile$|LICENSE$|README\.md$|SECURITY\.md$|THIRD_PARTY_NOTICES\.md$|renovate\.json$)'

case "$EVENT:$REF" in
  schedule:* | workflow_dispatch:* | *:refs/tags/v*) stage2=true stage3=true ;;
  pull_request:* | push:refs/heads/main) stage2=true stage3=false ;;
  *) stage2=false stage3=false ;;
esac

e2e=true minecraft=true sqlc=true code=true
if [ "$stage3" = false ] && git cat-file -e "${BASE:-none}^{commit}" 2>/dev/null; then
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  if git -c core.quotePath=false diff --no-renames --name-only -z "$BASE" HEAD >"$tmp/z"; then
    # One path per line; a newline inside a name becomes \001, so the
    # name stays one line and keeps its leading directories.
    tr '\n\000' '\001\n' <"$tmp/z" >"$tmp/paths"
    # Due unless grep finds no line; a grep error counts as due.
    due() {
      rc=0
      grep -qE "$1" "$tmp/paths" || rc=$?
      [ "$rc" -eq 1 ] && echo false || echo true
    }
    # Only when every path is known (grep -v finds no line) the stages
    # follow the paths; otherwise all stay on.
    rc=0
    grep -vE "$E2E|$MINECRAFT|$SQLC|$NONE|$DOCS" "$tmp/paths" >/dev/null || rc=$?
    if [ "$rc" -eq 1 ]; then
      e2e=$(due "$E2E")
      minecraft=$(due "$MINECRAFT")
      sqlc=$(due "$SQLC")
      # Code unless every path is documentation that no test reads.
      rc=0
      grep -vE "$DOCS" "$tmp/paths" >/dev/null || rc=$?
      if [ "$rc" -eq 1 ]; then
        code=$(due "$DOCTESTED")
      fi
    fi
  fi
fi

echo "stage2=$stage2"
echo "stage3=$stage3"
echo "e2e=$e2e"
echo "minecraft=$minecraft"
echo "sqlc=$sqlc"
echo "code=$code"
