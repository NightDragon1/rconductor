#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Formatting of the given files (NFR-013): gofmt for Go, Prettier for the
# web files. Used by the git hooks on wip/* branches, where the tests are
# left out.
set -euo pipefail
cd "$(dirname "$0")/.."
go_files=() web_files=()
for f in "$@"; do
  case "$f" in
    *.go) go_files+=("$f") ;;
    web/*) web_files+=("${f#web/}") ;;
  esac
done
if ((${#go_files[@]})); then
  unformatted=$(gofmt -l "${go_files[@]}")
  if [[ -n "$unformatted" ]]; then
    echo "not formatted:"
    echo "$unformatted"
    exit 1
  fi
fi
if ((${#web_files[@]})); then
  (cd web && npx prettier --check --ignore-unknown "${web_files[@]}")
fi
echo "formatting passed"
