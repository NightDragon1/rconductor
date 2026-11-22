#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Size labels of a pull request (PRJ-005), like the stage labels:
#   classify-size.sh SIZE < CURRENT_LABELS
# SIZE is within, over-tests or over from classify.sh; CURRENT_LABELS has
# one label per line. Prints the arguments for gh pr edit, one per line:
# over-limit for over, over-limit-tests for over-tests, and the removal of
# the other or both when the pull request is back within. An empty SIZE
# (count unknown) changes nothing.
set -euo pipefail
size=${1-}
case "$size" in
  over) want=over-limit ;;
  over-tests) want=over-limit-tests ;;
  within) want= ;;
  '') exit 0 ;;
  *) echo "classify-size: unknown size: $size" >&2; exit 2 ;;
esac
labels=$(cat)
has() { printf '%s\n' "$labels" | grep -qxF "$1"; }
for l in over-limit over-limit-tests; do
  if [[ $l == "$want" ]]; then
    has "$l" || printf '%s\n' --add-label "$l"
  elif has "$l"; then
    printf '%s\n' --remove-label "$l"
  fi
done
