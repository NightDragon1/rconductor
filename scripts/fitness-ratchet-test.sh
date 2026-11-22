#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Tests scripts/fitness-ratchet.sh (PRJ-005) in a throwaway repository: the
# lists may shrink, a new entry in either list fails, and an unknown base or
# a list missing at the base fails instead of passing.
set -euo pipefail
# A hook sets these; the throwaway repository must not reach the caller.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_COMMON_DIR GIT_OBJECT_DIRECTORY GIT_PREFIX
src=$(cd "$(dirname "$0")" && pwd)/fitness-ratchet.sh
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fails=0
cd "$tmp"
git init -q .
git config user.name test
git config user.email test@example.invalid
mkdir -p scripts tools/fitness
cp "$src" scripts/
printf '# head\ncmd internal/a # reason\ncmd internal/b # reason\n' >tools/fitness/exceptions.txt
printf '# head\ninternal/auth # reason\n' >tools/fitness/secret-destinations.txt
git add -A
git commit -qm base
base=$(git rev-parse HEAD)

# expect <name> <want exit> : runs the ratchet against $base.
expect() {
  local got=0
  scripts/fitness-ratchet.sh "$3" >/dev/null 2>&1 || got=$?
  if [ "$got" -ne "$2" ]; then
    echo "FAIL $1: exit $got, want $2" >&2
    fails=$((fails + 1))
  fi
}

expect "unchanged lists" 0 "$base"
printf '# head\ncmd internal/a # reason\n' >tools/fitness/exceptions.txt
expect "a shrunk exception list" 0 "$base"
printf '# head\ncmd internal/a # reason\ncmd internal/c # new\n' >tools/fitness/exceptions.txt
expect "a new exception" 1 "$base"
git checkout -q -- tools/fitness/exceptions.txt
printf '# head\ninternal/auth # reason\ninternal/web # new\n' >tools/fitness/secret-destinations.txt
expect "a new secret destination" 1 "$base"
git checkout -q -- tools/fitness/secret-destinations.txt
expect "an unknown base" 1 "0000000000000000000000000000000000000000"
git rm -q tools/fitness/secret-destinations.txt
git commit -qm "no list"
nolist=$(git rev-parse HEAD)
git checkout -q "$base" -- tools/fitness/secret-destinations.txt
expect "a list missing at the base" 1 "$nolist"

if [ "$fails" -ne 0 ]; then
  echo "fitness ratchet: $fails failed" >&2
  exit 1
fi
echo "fitness ratchet ok"
