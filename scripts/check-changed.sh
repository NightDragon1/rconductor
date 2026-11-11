#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Pre-commit gate: lint and tests of what is staged, Go with the race
# detector. The full gate is scripts/check.sh before each push.
set -euo pipefail
cd "$(dirname "$0")/.."

staged=$(git diff --cached --name-only --diff-filter=ACMR)

# The secret check runs on every branch.
scripts/check-secrets.sh

# On a wip/* branch only formatting and the secret check run; the tests of
# work in progress come with the pull request that finishes it.
if [[ "$(scripts/hook-scope.sh commit)" == wip ]]; then
  mapfile -t files <<<"$staged"
  scripts/check-format.sh "${files[@]}"
  echo "wip branch: tests left out"
  exit 0
fi
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
  # Directories whose files are all excluded by build constraints, such as
  # scripts/loadtest (//go:build ignore), are no package for lint and test;
  # vet their files directly instead.
  pkg_dirs=""
  for d in $go_dirs; do
    # Packages of a nested module (tools/) are checked inside that module.
    mod=$(cd "$d" && go env GOMOD)
    if [[ "$mod" != "$PWD/go.mod" ]]; then
      moddir=$(dirname "$mod")
      rel=./$(realpath --relative-to="$moddir" "$d")
      (cd "$moddir" && go tool golangci-lint run "$rel" && CGO_ENABLED=1 go test -race "$rel")
      continue
    fi
    if [[ -n $(go list -e -f '{{if or .GoFiles .TestGoFiles .XTestGoFiles}}pkg{{end}}' "$d") ]]; then
      pkg_dirs+="$d "
    else
      go vet "$d"/*.go
    fi
  done
  if [[ -n "$pkg_dirs" ]]; then
    # shellcheck disable=SC2086
    go tool -modfile=tools/go.mod golangci-lint run $pkg_dirs
    # shellcheck disable=SC2086
    CGO_ENABLED=1 go test -race $pkg_dirs
  fi
fi

if [[ -n "$web_files" ]]; then
  # shellcheck disable=SC2086
  (cd web && npx eslint $web_files && npx prettier --check $web_files && npx vitest related --run $web_files)
fi

echo "changed files passed"
