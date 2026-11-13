#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Checks scripts/check-git-config.sh (PRJ-005) on a throwaway repository:
# the fixed values pass, each deviation stops.
set -euo pipefail
cd "$(dirname "$0")/.."
check=$PWD/scripts/check-git-config.sh
# Run from a git hook, these point at the calling clone.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_COMMON_DIR GIT_OBJECT_DIRECTORY GIT_PREFIX
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fails=0
fresh() {
  rm -rf "$tmp/r" && git init -q "$tmp/r"
  git -C "$tmp/r" config remote.origin.url https://github.com/NightDragon1/rconductor.git
  git -C "$tmp/r" config remote.origin.fetch '+refs/heads/*:refs/remotes/origin/*'
  git -C "$tmp/r" config core.hooksPath .githooks
}
expect() { # label, want ok|stop
  local got=ok
  "$check" "$tmp/r" 2>/dev/null || got=stop
  [ "$got" = "$2" ] && echo "ok   $1" || { echo "FAIL $1: $got" >&2; fails=$((fails + 1)); }
}
fresh
expect "fixed values" ok
for kv in "core.bare true" "remote.origin.url https://example.invalid/r.git" "core.hooksPath /tmp" \
  "remote.origin.fetch +refs/heads/main:refs/heads/main" "remote.origin.mirror false" "remote.origin.push refs/heads/main"; do
  fresh
  # shellcheck disable=SC2086
  git -C "$tmp/r" config $kv
  expect "${kv%% *} changed" stop
done
fresh
git -C "$tmp/r" config --unset core.hooksPath
expect "core.hooksPath missing" stop
[ "$fails" -eq 0 ] || exit 1
echo "git config check ok"
