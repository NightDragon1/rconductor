#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# PreToolUse hook of Claude Code (NFR-013): runs tools/claudeguard on the
# hook input from stdin. The guard is built once per source hash into
# ${XDG_CACHE_HOME:-$HOME/.cache}/rconductor-claudeguard/<hash>/ and reused.
# Fails closed: when the build or the guard fails, the call is denied
# (exit 2, reason on stderr).
set -uo pipefail

deny() {
  echo "RCONductor guard: $1" >&2
  exit 2
}

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd) || deny "repository not found"
tools="$root/tools"
[ -d "$tools/claudeguard" ] || deny "guard source $tools/claudeguard not found"

hash=$(cat "$tools/go.mod" "$tools/go.sum" "$tools"/claudeguard/*.go 2>/dev/null | sha256sum) ||
  deny "guard source could not be hashed"
hash=${hash%% *}
[ ${#hash} -eq 64 ] || deny "guard source could not be hashed"

cache="${XDG_CACHE_HOME:-$HOME/.cache}/rconductor-claudeguard/${hash:0:32}"
bin="$cache/claudeguard"

if [ ! -x "$bin" ]; then
  mkdir -p "$cache" || deny "cache directory $cache could not be created"
  tmp=$(mktemp "$cache/build.XXXXXX") || deny "cache directory $cache is not writable"
  if ! out=$(cd "$tools" && CGO_ENABLED=0 GOFLAGS=-mod=readonly go build -o "$tmp" ./claudeguard 2>&1); then
    rm -f "$tmp"
    deny "the guard could not be built, so the call is denied: ${out:0:500}"
  fi
  mv -f "$tmp" "$bin" || deny "the guard could not be installed in $cache"
fi

# The clone of the guard: the only place for git config repairs.
RCD_GUARD_ROOT=$root "$bin"
rc=$?
case $rc in
  0 | 2) exit "$rc" ;;
  *) deny "the guard failed with status $rc, so the call is denied" ;;
esac
