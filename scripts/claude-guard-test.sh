#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Tests the Claude Code PreToolUse hook (.claude/hooks/guard.sh) end to end
# (NFR-013, C34-C38): hook JSON goes to the real hook command, which builds
# tools/claudeguard into a temporary cache and runs it. Also checks that the
# hook denies when the guard cannot be built. No command from the cases is
# executed. Optional argument: path of the hook to test.
set -euo pipefail
repo=$(cd "$(dirname "$0")/.." && pwd)
hook=$(realpath -e -- "${1:-$repo/.claude/hooks/guard.sh}")

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fail=0

# hook_run <json> [env...]: runs the hook; prints the status, stderr in $tmp/err.
hook_run() {
  local json=$1
  shift
  set +e
  printf '%s' "$json" | env "$@" bash "$hook" >"$tmp/out" 2>"$tmp/err"
  local rc=$?
  set -e
  echo "$rc"
}

# Bash cases run in an empty repository: git push reads its configuration.
# Run from a git hook, these point at the calling clone, and git init
# would reinitialise it.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_COMMON_DIR GIT_OBJECT_DIRECTORY GIT_PREFIX
git init -q "$tmp/repo"
bash_json() { jq -nc --arg c "$1" --arg w "$tmp/repo" '{tool_name: "Bash", cwd: $w, tool_input: {command: $c}}'; }
file_json() { jq -nc --arg t "$1" --arg p "$2" '{tool_name: $t, cwd: "/w", tool_input: {file_path: $p}}'; }

# expect <case> <want status> <json>
expect() {
  local rc
  rc=$(hook_run "$3" XDG_CACHE_HOME="$tmp/cache")
  if [ "$rc" = "$2" ]; then
    echo "ok   $1"
  else
    echo "FAIL $1: status $rc, want $2 ($(head -c 300 "$tmp/err"))"
    fail=1
  fi
}

start=$(date +%s%N)
expect "C34 bundled force push is denied (first call builds the guard)" 2 "$(bash_json 'git push -vvf origin main')"
built=$(date +%s%N)
expect "C34 force push behind a global option is denied" 2 "$(bash_json 'git -P push -f origin main')"
expect "C35 gh api with a bundled method flag is denied" 2 "$(bash_json 'gh api -iX DELETE repos/x/y')"
expect "C36 git config remote write is denied" 2 "$(bash_json 'git config remote.origin.mirror true')"
expect "C36 Write on .git/config is denied" 2 "$(file_json Write /w/.git/config)"
expect "C38 gh auth token is denied" 2 "$(bash_json 'gh auth token')"
expect "plain push is allowed" 0 "$(bash_json 'git push -u origin feat/x')"
expect "run watch loop is allowed" 0 "$(bash_json 'timeout 1800 gh run watch $id --exit-status')"
expect "Edit on a source file is allowed" 0 "$(file_json Edit /w/internal/x.go)"
expect "broken hook input is denied" 2 "not json"
done_at=$(date +%s%N)

if grep -q "RCONductor guard:" "$tmp/err"; then
  echo "ok   denial reason is on stderr"
else
  echo "FAIL denial reason is on stderr: $(cat "$tmp/err")"
  fail=1
fi
if [ "$(find "$tmp/cache/rconductor-claudeguard" -name claudeguard -type f | wc -l)" = 1 ]; then
  echo "ok   the guard is built once into the cache"
else
  echo "FAIL the guard is built once into the cache"
  fail=1
fi
echo "info first call $(((built - start) / 1000000)) ms, 9 cached calls $(((done_at - built) / 1000000)) ms"

# Without a Go toolchain the guard cannot be built: the hook denies.
mkdir -p "$tmp/bin"
for t in bash cat sha256sum mkdir mktemp mv rm dirname env; do
  ln -s "$(command -v "$t")" "$tmp/bin/$t"
done
rc=$(hook_run "$(bash_json 'git status')" XDG_CACHE_HOME="$tmp/empty-cache" PATH="$tmp/bin")
if [ "$rc" = 2 ] && grep -q "could not be built" "$tmp/err"; then
  echo "ok   a failed build denies the call"
else
  echo "FAIL a failed build denies the call: status $rc ($(head -c 300 "$tmp/err"))"
  fail=1
fi

exit "$fail"
