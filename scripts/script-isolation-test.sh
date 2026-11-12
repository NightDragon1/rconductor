#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# The script tests keep to their own throwaway repositories (NFR-013): run
# as git runs a hook (GIT_DIR, GIT_WORK_TREE and GIT_INDEX_FILE set), each
# scripts/*-test.sh leaves the configuration (identity included), the index
# and the work tree of the calling clone as they were. Runs them in a local
# clone, never in the real one.
set -euo pipefail
src=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX
clone=$tmp/clone
git clone -q --no-hardlinks "$src" "$clone"
# The work tree under test as it is now, including uncommitted scripts.
cp -a "$src/scripts/." "$clone/scripts/"
git -C "$clone" config user.name "isolation check"
git -C "$clone" config user.email isolation@example.invalid
git -C "$clone" add -A && git -C "$clone" commit -qm "scripts under test" --allow-empty
state() {
  {
    cat "$clone/.git/config"
    git -C "$clone" rev-parse HEAD
    git -C "$clone" ls-files -s | sha256sum
    git -C "$clone" status --porcelain=v1 --untracked-files=all
  } 2>&1
}
before=$(state)
fails=0
for t in "$clone"/scripts/*-test.sh; do
  name=$(basename "$t")
  case "$name" in script-isolation-test.sh) continue ;; esac
  (cd "$clone" && GIT_DIR=$clone/.git GIT_WORK_TREE=$clone GIT_INDEX_FILE=$clone/.git/index \
    bash "scripts/$name" >/dev/null 2>&1) || true
  after=$(state)
  if [ "$after" != "$before" ]; then
    echo "$name changed the calling clone:" >&2
    diff <(printf '%s\n' "$before") <(printf '%s\n' "$after") | head -n 10 >&2 || true
    fails=$((fails + 1))
    before=$after
  fi
done
[ "$fails" -eq 0 ] || exit 1
echo "script isolation ok"
