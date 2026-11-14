#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Before every push (pre-push hook, PRJ-005): the local configuration of
# the clone must have its fixed values, otherwise the push stops. The same
# values may be set again from a tool call (tools/claudeguard/repair.go).
# Optional argument: the directory of the clone or a worktree of it.
set -euo pipefail
dir=${1:-.}
fail=0
want() { # key, value
  local got
  got=$(git -C "$dir" config --local --get "$1" 2>/dev/null || true)
  if [ "$got" != "$2" ]; then
    echo "git config $1 is [$got], want [$2]: git config $1 '$2'" >&2
    fail=1
  fi
}
want core.bare false
want remote.origin.url https://github.com/NightDragon1/rconductor.git
want remote.origin.fetch '+refs/heads/*:refs/remotes/origin/*'
want core.hooksPath .githooks
for k in remote.origin.mirror remote.origin.push; do
  if git -C "$dir" config --local --get-all "$k" >/dev/null 2>&1; then
    echo "git config $k is set: git config --unset-all $k" >&2
    fail=1
  fi
done
[ "$fail" -eq 0 ] || { echo "the clone's configuration differs from its fixed values; push stopped" >&2; exit 1; }
