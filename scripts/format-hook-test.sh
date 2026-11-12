#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Tests the Claude Code format hook (.claude/hooks/format.sh) with stub
# gofmt and prettier that only record their arguments (NFR-013, C31).
# Optional argument: path of the hook to test (default: the repo's hook).
set -euo pipefail
repo=$(cd "$(dirname "$0")/.." && pwd)
hook=$(realpath -e -- "${1:-$repo/.claude/hooks/format.sh}")

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
root="$tmp/root"
log="$tmp/calls.log"
mkdir -p "$tmp/bin" "$tmp/outside" "$root/internal/srv" "$root/internal/api" \
  "$root/web/src/api" "$root/web/node_modules/.bin"
for tool in "$tmp/bin/gofmt" "$root/web/node_modules/.bin/prettier"; do
  printf '#!/bin/sh\necho "%s $*" >>"%s"\n' "$(basename "$tool")" "$log" >"$tool"
  chmod +x "$tool"
done
touch "$root/internal/srv/a.go" "$root/web/src/b.ts" "$root/internal/c.ts" \
  "$root/internal/api/gen.go" "$root/web/src/api/schema.d.ts" \
  "$tmp/outside/d.go" "$tmp/outside/e.ts" "$tmp/x.ts"

fail=0
# run <case> <file> <expected tool or "none">
run() {
  : >"$log"
  jq -n --arg f "$2" '{tool_input: {file_path: $f}}' |
    CLAUDE_PROJECT_DIR="$root" PATH="$tmp/bin:$PATH" "$hook"
  local got
  got=$(cut -d' ' -f1 "$log" | sort -u | tr '\n' ' ')
  got=${got% }
  if [ "$3" = none ]; then
    if [ -n "$got" ]; then
      echo "FAIL $1: $2 was formatted ($(cat "$log"))"
      fail=1
      return
    fi
  elif [ "$got" != "$3" ]; then
    echo "FAIL $1: $2 expected $3, got '${got}'"
    fail=1
    return
  fi
  echo "ok   $1"
}

run "C31 Go file in the repo is formatted" "$root/internal/srv/a.go" gofmt
run "C31 web file in the repo is formatted" "$root/web/src/b.ts" prettier
run "C31 Go file outside the repo is left alone" "$tmp/outside/d.go" none
run "C31 web file outside the repo is left alone" "$tmp/outside/e.ts" none
run "C31 path leaving the repo through web/.. is left alone" "$root/web/../../x.ts" none
run "C31 path leaving web/ through .. gets no prettier" "$root/web/../internal/c.ts" none
run "C31 generated Go code is left alone" "$root/internal/api/gen.go" none
run "C31 generated Go code reached through .. is left alone" "$root/web/../internal/api/gen.go" none
run "C31 generated web code is left alone" "$root/web/src/api/schema.d.ts" none
run "C31 missing file is left alone" "$root/internal/srv/missing.go" none

# The stubs must have received the file after "--".
: >"$log"
jq -n --arg f "$root/internal/srv/a.go" '{tool_input: {file_path: $f}}' |
  CLAUDE_PROJECT_DIR="$root" PATH="$tmp/bin:$PATH" "$hook"
if grep -q -- "-- $root/internal/srv/a.go" "$log"; then
  echo "ok   C31 file argument follows --"
else
  echo "FAIL C31 file argument follows --: $(cat "$log")"
  fail=1
fi

exit "$fail"
