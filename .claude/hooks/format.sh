#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# PostToolUse hook of Claude Code: formats the file an edit just changed,
# Go with gofmt, the web interface with prettier. Generated code
# (internal/api/, web/src/api/) stays as the generators wrote it. Without
# web/node_modules prettier is skipped; the hooks and the CI check anyway.
set -uo pipefail
root=${CLAUDE_PROJECT_DIR:-$(git rev-parse --show-toplevel 2>/dev/null)}
file=$(jq -r '.tool_input.file_path // empty' 2>/dev/null) || exit 0
[ -n "$file" ] && [ -f "$file" ] || exit 0
case "$file" in
  "$root"/internal/api/* | "$root"/web/src/api/*) exit 0 ;;
  *.go) gofmt -w "$file" ;;
  "$root"/web/*.ts | "$root"/web/*.tsx | "$root"/web/*.json | "$root"/web/*.css | "$root"/web/*.md)
    [ -x "$root/web/node_modules/.bin/prettier" ] &&
      (cd "$root/web" && node_modules/.bin/prettier --write --log-level warn "$file") ;;
esac
exit 0
