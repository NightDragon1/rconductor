#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# The fitness exception list may only shrink (PRJ-005): fails on an entry
# the base commit does not have (none at the base: nothing to compare).
set -euo pipefail
cd "$(dirname "$0")/.."
base=${1:?base commit}
file=tools/fitness/exceptions.txt
keys() { sed 's/#.*//' | awk 'NF == 2 {print $1, $2}' | sort -u; }
if ! old=$(git show "$base:$file" 2>/dev/null); then
  echo "no exception list at $base"
  exit 0
fi
added=$(comm -13 <(printf '%s\n' "$old" | keys) <(keys <"$file"))
if [ -n "$added" ]; then
  echo "new fitness exceptions are not allowed:" >&2
  printf '%s\n' "$added" >&2
  exit 1
fi
echo "fitness exceptions did not grow"
