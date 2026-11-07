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
# Nightly, manual and tag runs do everything. Without a usable base commit
# (first push of a branch, force push) all paths count as changed.
# Needs EVENT, REF and BASE from the workflow.
set -eu
E2E='^(web/|api/|internal/|cmd/|migrations/|profiles/|go\.(mod|sum)$|Makefile$|\.github/workflows/|scripts/ci-scope\.sh$)'
MINECRAFT='^(internal/(rcon|connmgr)/|internal/app/minecraft_test\.go$|profiles/|scripts/minecraft|\.github/workflows/|scripts/ci-scope\.sh$)'
SQLC='^(migrations/|internal/store/queries/|internal/store/sqlitedb/|sqlc\.yaml$)'

case "$EVENT:$REF" in
  schedule:* | workflow_dispatch:* | *:refs/tags/v*) stage2=true stage3=true ;;
  pull_request:* | push:refs/heads/main) stage2=true stage3=false ;;
  *) stage2=false stage3=false ;;
esac

e2e=true minecraft=true sqlc=true
if [ "$stage3" = false ] && git cat-file -e "${BASE:-none}^{commit}" 2>/dev/null; then
  changed=$(git diff --name-only "$BASE" HEAD)
  printf '%s\n' "$changed" | grep -qE "$E2E" || e2e=false
  printf '%s\n' "$changed" | grep -qE "$MINECRAFT" || minecraft=false
  printf '%s\n' "$changed" | grep -qE "$SQLC" || sqlc=false
fi

echo "stage2=$stage2"
echo "stage3=$stage3"
echo "e2e=$e2e"
echo "minecraft=$minecraft"
echo "sqlc=$sqlc"
