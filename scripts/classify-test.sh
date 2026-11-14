#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Checks scripts/classify.sh (PRJ-005) against the real class file: stage,
# areas, changed lines and the size limit of stage 2, with renames,
# deletions, generated files and glob edge cases.
set -euo pipefail
src=$(cd "$(dirname "$0")/.." && pwd)
class=$src/.github/core-paths.yml
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fails=0
T=$'\t'

# check NAME WANT_EXIT WANT_OUTPUT FILE_LINE...
# Each FILE_LINE is "status additions deletions filename [previous]",
# separated by spaces; WANT_OUTPUT is "stage areas lines".
check() {
  local name=$1 want_rc=$2 want=$3 rc=0 got
  shift 3
  : >"$tmp/in"
  for l in "$@"; do printf '%s\n' "${l// /$T}" >>"$tmp/in"; done
  got=$("$src/scripts/classify.sh" "$class" "$tmp/in" 2>"$tmp/err") || rc=$?
  got=$(printf '%s\n' "$got" | sed 's/^[a-z]*=//' | paste -sd' ')
  if [[ $rc != "$want_rc" || $got != "$want" ]]; then
    echo "$name: want exit $want_rc [$want], got exit $rc [$got] $(cat "$tmp/err")" >&2
    fails=$((fails + 1))
  fi
}

check "plain change" 0 "stage-1  12" "modified 10 2 internal/servers/servers.go"
check "docs and web" 0 "stage-1  3" "added 1 0 docs/user/a.md" "modified 1 1 web/src/App.tsx"
check "core file" 0 "stage-2 authz 5" "modified 4 1 internal/perm/perm.go"
check "deep core file" 0 "stage-2 authz 1" "added 1 0 internal/perm/x/y.go"
check "similar prefix" 0 "stage-1  1" "added 1 0 internal/permx/a.go"
check "rename out of core" 0 "stage-2 cmd 0" "renamed 0 0 internal/other/a.go internal/rcon/a.go"
check "rename into core" 0 "stage-2 render 0" "renamed 0 0 internal/foreign/b.go internal/other/b.go"
check "deletion of core file" 0 "stage-2 audit 30" "removed 0 30 internal/audit/audit.go"
check "only generated" 0 "stage-1  0" "modified 900 100 internal/api/api.gen.go" \
  "modified 50 50 internal/store/sqlitedb/x.sql.go" "modified 300 0 web/src/api/schema.d.ts"
check "generated lines not counted" 0 "stage-2 boundary 400" "modified 399 1 api/openapi.yaml" \
  "modified 900 100 internal/api/api.gen.go"
check "stage 2 at 400" 0 "stage-2 input 400" "modified 200 100 internal/pattern/pattern.go" \
  "modified 100 0 internal/servers/servers.go"
check "stage 2 at 401" 1 "stage-2 input 401" "modified 200 100 internal/pattern/pattern.go" \
  "modified 101 0 internal/servers/servers.go"
check "stage 1 over 400" 0 "stage-1  5000" "modified 5000 0 internal/servers/servers.go"
check "move into generated counts" 0 "stage-2 cmd 7" "renamed 7 0 internal/api/c.go internal/commands/c.go"
check "workflow change" 0 "stage-2 boundary 2" "modified 1 1 .github/workflows/ci.yml"
check "class file change" 0 "stage-2 boundary 1" "modified 1 0 .github/core-paths.yml"
check "rules files" 0 "stage-2 rules 4" "modified 1 0 CLAUDE.md" "modified 1 0 scripts/release-notes.sh" \
  "modified 1 0 .githooks/pre-push" "modified 1 0 tools/claudeguard/x/y.go"
check "not a release script" 0 "stage-1  1" "modified 1 0 scripts/check-spdx.sh"
check "gate scripts and test data" 0 "stage-2 rules 4" "modified 1 0 scripts/classify.sh" \
  "modified 1 0 scripts/ci-scope-test.sh" "modified 1 0 scripts/go-test-shard.sh" \
  "added 1 0 web/scripts/testdata/render-narrow/src/Hidden.tsx"
check "npm config" 0 "stage-2 rules 1" "added 1 0 web/.npmrc"
check "exact file only" 0 "stage-1  2" "modified 1 0 internal/logging/witness.go" "added 1 0 docs/CLAUDE.md"
check "several areas" 0 "stage-2 audit,redact 2" "modified 1 0 internal/logging/events.go" \
  "modified 1 0 internal/logging/redact.go"
check "migration" 0 "stage-2 boundary 9" "added 9 0 migrations/sqlite/00099_x.sql"
check "escaped name" 0 "stage-2 authz 1" 'added 1 0 internal/auth/a\tb.go'
check "empty list" 0 "stage-1  0"
check "bad line" 2 "" "modified x 0 a.go"

# The class file is read strictly: a line outside the format fails.
printf 'authz:\n  - internal/perm/**\nfoo bar\n' >"$tmp/bad.yml"
rc=0
"$src/scripts/classify.sh" "$tmp/bad.yml" /dev/null >/dev/null 2>&1 || rc=$?
[[ $rc == 2 ]] || { echo "bad class file: want exit 2, got $rc" >&2; fails=$((fails + 1)); }

if ((fails)); then
  echo "classify-test: $fails failed" >&2
  exit 1
fi
echo "classify-test passed"
