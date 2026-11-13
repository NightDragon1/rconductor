#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# The tests keep to their own throwaway repositories (NFR-013, PRJ-005):
# run as git runs a hook (GIT_DIR, GIT_WORK_TREE and GIT_INDEX_FILE set),
# each scripts/*-test.sh and the Go tests of every package that runs git
# (always tools/claudeguard) leave the configuration (identity included),
# the index and the work tree of the calling clone as they were. Runs them
# in a local clone, never in the real one.
set -euo pipefail
src=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX
clone=$tmp/clone
git clone -q --no-hardlinks "$src" "$clone"
# The work tree under test as it is now, including uncommitted scripts.
cp -a "$src/scripts/." "$clone/scripts/"
cp -a "$src/tools/." "$clone/tools/"
git -C "$clone" config user.name "isolation check"
git -C "$clone" config user.email isolation@example.invalid
git -C "$clone" add -A && git -C "$clone" commit -qm "scripts under test" --allow-empty
# A hook in a worktree runs with GIT_DIR at <clone>/.git/worktrees/<name>
# and no work tree; each test runs in both forms.
git -C "$clone" worktree add -q "$tmp/wt" HEAD
wtgit=$(git -C "$tmp/wt" rev-parse --absolute-git-dir)
state() {
  {
    cat "$clone/.git/config"
    git -C "$clone" rev-parse HEAD
    git -C "$clone" ls-files -s | sha256sum
    git -C "$clone" status --porcelain=v1 --untracked-files=all
  } 2>&1 || true # a broken clone shows as a difference, not as an abort
}
before=$(state)
fails=0
for t in "$clone"/scripts/*-test.sh; do
  name=$(basename "$t")
  case "$name" in script-isolation-test.sh) continue ;; esac
  (cd "$clone" && GIT_DIR=$clone/.git GIT_WORK_TREE=$clone GIT_INDEX_FILE=$clone/.git/index \
    bash "scripts/$name" >/dev/null 2>&1) || true
  (cd "$tmp/wt" && GIT_DIR=$wtgit bash "scripts/$name" >/dev/null 2>&1) || true
  after=$(state)
  if [ "$after" != "$before" ]; then
    echo "$name changed the calling clone:" >&2
    diff <(printf '%s\n' "$before") <(printf '%s\n' "$after") | head -n 10 >&2 || true
    fails=$((fails + 1))
    before=$after
  fi
done
# Go packages that run git: their tests must refuse or isolate the
# variables, whatever their result.
gopkgs=$( (
  cd "$clone"
  echo tools/claudeguard
  grep -rlE --include='*.go' 'exec\.Command(Context)?\([^)]*"git"' internal cmd tools 2>/dev/null | xargs -r -n1 dirname || true
) | sort -u)
for d in $gopkgs; do
  mod=$(cd "$clone/$d" && go env GOMOD)
  (cd "$(dirname "$mod")" && GIT_DIR=$clone/.git GIT_WORK_TREE=$clone GIT_INDEX_FILE=$clone/.git/index \
    go test -count=1 "./$(realpath --relative-to="$(dirname "$mod")" "$clone/$d")" >/dev/null 2>&1) || true
  after=$(state)
  if [ "$after" != "$before" ]; then
    echo "go test $d changed the calling clone:" >&2
    diff <(printf '%s\n' "$before") <(printf '%s\n' "$after") | head -n 10 >&2 || true
    fails=$((fails + 1))
    before=$after
  fi
done

[ "$fails" -eq 0 ] || exit 1
echo "script isolation ok"
