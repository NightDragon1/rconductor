#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# The fitness exception lists may only shrink (PRJ-005): fails on an entry
# the base commit does not have, in tools/fitness/exceptions.txt (rule and
# symbol) and in tools/fitness/secret-destinations.txt (package). Fails
# closed: a base that cannot be read, or a list missing at the base, is an
# error, not a pass.
set -euo pipefail
cd "$(dirname "$0")/.."
base=${1:?base commit}
if ! git cat-file -e "$base^{commit}" 2>/dev/null; then
  echo "base commit $base not found: the lists cannot be compared" >&2
  exit 1
fi
fail=0
# check <file> <n>: the first <n> words of an entry are its key.
check() {
  local file=$1 n=$2 old added
  keys() { sed 's/#.*//' | awk -v n="$n" 'NF >= n {k = $1; for (i = 2; i <= n; i++) k = k " " $i; print k}' | sort -u; }
  if ! old=$(git show "$base:$file" 2>/dev/null); then
    echo "no $file at $base: the list cannot be compared" >&2
    fail=1
    return
  fi
  added=$(comm -13 <(printf '%s\n' "$old" | keys) <(keys <"$file"))
  if [ -n "$added" ]; then
    echo "new entries in $file are not allowed:" >&2
    printf '%s\n' "$added" >&2
    fail=1
  fi
}
check tools/fitness/exceptions.txt 2
check tools/fitness/secret-destinations.txt 1
exit "$fail"
