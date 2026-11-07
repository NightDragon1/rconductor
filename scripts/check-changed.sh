#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Pre-commit gate: lint and tests of what is staged, Go with the race
# detector. The full gate is scripts/check.sh before each push.
set -euo pipefail
cd "$(dirname "$0")/.."

staged=$(git diff --cached --name-only --diff-filter=ACMR)
go_dirs=$(printf '%s\n' "$staged" | grep -E '\.go$' | xargs -r -n1 dirname | sort -u | sed 's|^|./|' || true)
web_files=$(printf '%s\n' "$staged" | grep -E '^web/.*\.(ts|tsx)$' | sed 's|^web/||' || true)

if [[ -n "$go_dirs" ]]; then
  # shellcheck disable=SC2086
  unformatted=$(gofmt -l $go_dirs)
  if [[ -n "$unformatted" ]]; then
    echo "not formatted:"
    echo "$unformatted"
    exit 1
  fi
  # shellcheck disable=SC2086
  go tool -modfile=tools/go.mod golangci-lint run $go_dirs
  # shellcheck disable=SC2086
  CGO_ENABLED=1 go test -race $go_dirs
fi

if [[ -n "$web_files" ]]; then
  # shellcheck disable=SC2086
  (cd web && npx eslint $web_files && npx prettier --check $web_files && npx vitest related --run $web_files)
fi

echo "changed files passed"
