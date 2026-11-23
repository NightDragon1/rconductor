#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Tests of scripts/pr-size.sh (PRJ-005) against the real class file:
# generated files do not count, a move into a generated file does, the
# limit is 400, labels are added and removed, bad lines are skipped.
set -uo pipefail
src=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fails=0

# check NAME WANT LABELS FILE_LINE...: WANT is the output, one line joined by spaces
check() {
  local name=$1 want=$2 labels=$3 got
  shift 3
  printf '%b\n' "$@" >"$tmp/in"
  got=$(printf '%s' "$labels" | tr ',' '\n' | "$src/scripts/pr-size.sh" "$src/.github/core-paths.yml" "$tmp/in" | paste -sd' ')
  if [[ $got != "$want" ]]; then
    echo "pr-size-test: $name: got '$got', want '$want'" >&2
    fails=$((fails + 1))
  fi
}

check "within" "lines=12 size=within" "" 'modified\t10\t2\tinternal/servers/servers.go\t'
check "at 400" "lines=400 size=within" "" 'modified\t300\t100\tinternal/perm/perm.go\t'
check "at 401" "lines=401 size=over --add-label over-limit" "" 'modified\t301\t100\tinternal/perm/perm.go\t'
check "label already there" "lines=401 size=over" "over-limit" 'modified\t301\t100\tinternal/perm/perm.go\t'
check "back within removes the label" "lines=1 size=within --remove-label over-limit" "over-limit,other" 'added\t1\t0\tdocs/a.md\t'
check "old label removed" "lines=1 size=within --remove-label over-limit-tests" "over-limit-tests" 'added\t1\t0\tdocs/a.md\t'
check "generated not counted" "lines=5 size=within" "" \
  'modified\t900\t100\tinternal/api/api.gen.go\t' 'modified\t800\t0\tweb/package-lock.json\t' \
  'modified\t50\t50\tinternal/store/sqlitedb/users.sql.go\t' 'modified\t5\t0\tapi/openapi.yaml\t'
check "move into generated counts" "lines=7 size=within" "" 'renamed\t7\t0\tinternal/api/api.gen.go\tinternal/commands/c.go'
check "bad line skipped" "lines=3 size=within" "" 'garbage' 'modified\tx\t1\ta.go\t' 'added\t3\t0\tb.go\t'
check "empty list" "lines=0 size=within" "" ''

if ((fails)); then
  echo "pr-size-test: $fails failed" >&2
  exit 1
fi
echo "pr-size-test passed"
