#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Checks the scope of the git hooks (NFR-013): on wip/* branches the hooks
# leave out the tests and still check formatting; on every other branch,
# and detached, they run in full. Runs the real hooks in a throwaway
# repository with stand-ins for go, gofmt, npx and check.sh that only
# record their calls.
set -euo pipefail
src=$(cd "$(dirname "$0")/.." && pwd)
fails=0
expect() { # what, want, got
  [ "$2" = "$3" ] || { echo "$1: want [$2], got [$3]" >&2; fails=$((fails + 1)); }
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
repo=$tmp/repo log=$tmp/calls
mkdir -p "$repo/scripts" "$repo/.githooks" "$repo/web/node_modules/.bin" "$repo/internal/a" "$tmp/bin"
cp "$src/scripts/hook-scope.sh" "$src/scripts/check-format.sh" "$src/scripts/check-changed.sh" "$repo/scripts/"
cp "$src/.githooks/pre-push" "$repo/.githooks/"
printf '#!/bin/sh\necho "check.sh $*" >>"%s"\n' "$log" > "$repo/scripts/check.sh"
for tool in go gofmt; do
  printf '#!/bin/sh\necho "%s $*" >>"%s"\n' "$tool" "$log" > "$tmp/bin/$tool"
done
printf '#!/bin/sh\necho "npx $*" >>"%s"\n' "$log" > "$repo/web/node_modules/.bin/npx"
cp "$repo/web/node_modules/.bin/npx" "$tmp/bin/npx"
chmod +x "$repo/scripts/"*.sh "$repo/.githooks/pre-push" "$tmp/bin/"* "$repo/web/node_modules/.bin/npx"
export PATH="$tmp/bin:$PATH"

git -C "$repo" init -q -b main
git -C "$repo" config user.name test
git -C "$repo" config user.email test@example.invalid
echo "package a" > "$repo/internal/a/a.go"
echo "export const b = 1;" > "$repo/web/b.ts"
git -C "$repo" add -A
git -C "$repo" commit -qm base

scope() { (cd "$repo" && scripts/hook-scope.sh "$@"); }
commit_hook() { # branch: runs the pre-commit check on a staged change
  git -C "$repo" checkout -q -B "$1"
  echo "// $1" >> "$repo/internal/a/a.go"
  echo "// $1" >> "$repo/web/b.ts"
  git -C "$repo" add -A
  : > "$log"
  (cd "$repo" && scripts/check-changed.sh >/dev/null 2>&1) || echo "check-changed failed on $1" >> "$log"
}
push_hook() { # refs line(s) on stdin of the pre-push hook
  : > "$log"
  (cd "$repo" && printf '%s\n' "$@" | .githooks/pre-push >/dev/null 2>&1) || echo "pre-push failed" >> "$log"
}

expect "scope on main" full "$(git -C "$repo" checkout -q main && scope commit)"
expect "scope on wip/x" wip "$(git -C "$repo" checkout -q -B wip/x && scope commit)"
expect "scope on feature" full "$(git -C "$repo" checkout -q -B feat/wip-thing && scope commit)"
expect "scope detached" full "$(git -C "$repo" checkout -q --detach && scope commit)"
expect "push of a wip branch" wip "$(printf 'refs/heads/wip/x a refs/heads/wip/x b\n' | scope push)"
expect "push of wip and main" full "$(printf 'refs/heads/wip/x a refs/heads/wip/x b\nrefs/heads/main a refs/heads/main b\n' | scope push)"
expect "push of a feature branch" full "$(printf 'refs/heads/f a refs/heads/f b\n' | scope push)"
expect "push to a wip name from another branch" wip "$(printf 'refs/heads/f a refs/heads/wip/f b\n' | scope push)"
expect "push of a tag" full "$(printf 'refs/tags/v1 a refs/tags/v1 b\n' | scope push)"
expect "push with no refs" full "$(printf '' | scope push)"

commit_hook wip/2026-01-01
expect "wip commit: gofmt runs" 1 "$(grep -c '^gofmt -l' "$log")"
expect "wip commit: prettier runs" 1 "$(grep -c 'prettier --check' "$log")"
expect "wip commit: no go test or lint" 0 "$(grep -c '^go ' "$log")"
expect "wip commit: no vitest or eslint" 0 "$(grep -cE 'vitest|eslint' "$log")"

commit_hook feat/x
expect "feature commit: go test runs" 1 "$(grep -c '^go test -race' "$log")"
expect "feature commit: vitest runs" 1 "$(grep -c 'vitest related' "$log")"

git -C "$repo" checkout -q wip/2026-01-01
git -C "$repo" commit -qam "wip: change"
push_hook "refs/heads/wip/2026-01-01 a refs/heads/wip/2026-01-01 b"
expect "wip push: no check.sh" 0 "$(grep -c '^check.sh' "$log")"
expect "wip push: gofmt runs" 1 "$(grep -c '^gofmt -l' "$log")"
push_hook "refs/heads/feat/x a refs/heads/feat/x b"
expect "feature push: check.sh without tests" 1 "$(grep -c '^check.sh --no-tests' "$log")"

[ "$fails" -eq 0 ] || exit 1
echo "hook scope ok"
