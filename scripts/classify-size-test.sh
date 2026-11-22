#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Tests of classify-size.sh and line-limit.sh (PRJ-005).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fails=0
labels() { # labels NAME SIZE "CURRENT" "WANT ARGS"
  local got
  got=$(printf '%s' "$3" | tr ',' '\n' | "$here/classify-size.sh" "$2" | paste -sd' ')
  if [[ $got != "$4" ]]; then echo "$1: want [$4], got [$got]" >&2; fails=$((fails + 1)); fi
}
labels "over through tests alone" over-tests "stage-2" "--add-label over-limit-tests"
labels "over in the code" over "stage-2,over-limit-tests" "--add-label over-limit --remove-label over-limit-tests"
labels "back within removes both" within "stage-2,over-limit,over-limit-tests" "--remove-label over-limit --remove-label over-limit-tests"
labels "label already there" over "stage-2,over-limit" ""
labels "unknown size changes nothing" "" "over-limit" ""
limit() { # limit NAME WANT_EXIT WANT_GREP ARGS...
  local name=$1 want=$2 grep=$3 rc=0
  shift 3
  "$here/line-limit.sh" "$@" "$tmp/summary" >"$tmp/out" 2>&1 || rc=$?
  if [[ $rc != "$want" ]] || ! grep -q -- "$grep" "$tmp/out"; then
    echo "$name: want exit $want and [$grep], got exit $rc: $(cat "$tmp/out")" >&2
    fails=$((fails + 1))
  fi
}
limit "over through tests warns" 0 "::warning::.*tests alone" over-tests 450 50 400
limit "over in the code warns" 0 "::warning::.*410 changed lines of code" over 420 410 10
limit "within" 0 "within the line limit" within 100 80 20
limit "unknown count fails" 1 "::error::line count unknown" ""
grep -q "450 in all, 50 without tests (400 in tests)" "$tmp/summary" || { echo "summary misses the counts" >&2; fails=$((fails + 1)); }
((fails == 0)) && echo "classify-size-test passed"
exit "$fails"
