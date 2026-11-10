#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Splits the Go packages into the shards of the CI job "Go test".
#   go-test-shard.sh N        prints the import paths of shard N
#   go-test-shard.sh --check [TOTAL [INDEX SHARD]]
#                             fails unless every package of go list ./...
#                             is in exactly one shard, with TOTAL (the
#                             matrix size) unless SHARDS matches it, and
#                             with INDEX (the job index from 0) and SHARD
#                             (its matrix value) unless SHARD is INDEX + 1;
#                             run in every job, that makes the matrix
#                             exactly 1 to SHARDS
# The slow packages are spread by name so that each shard takes about the
# same time; the last shard takes everything not listed, so a new package
# lands there by itself. Times come from the CI logs (go test -race, per
# package): web about 140 s, app 120 s, auth 105 s; each of them bounds
# its shard.
set -eu
cd "$(dirname "$0")/.."

SHARDS=3
MODULE=$(go list -m)
SHARD1="internal/web internal/users internal/audit internal/cache"
SHARD2="internal/app internal/commands internal/servers cmd/panel"

listed() {
  case "$1" in
    1) words=$SHARD1 ;;
    2) words=$SHARD2 ;;
    *) words="" ;;
  esac
  for p in $words; do echo "$MODULE/$p"; done
}

shard() {
  if [ "$1" -lt "$SHARDS" ]; then
    listed "$1"
  else
    all=$(go list ./...)
    named=$(i=1; while [ "$i" -lt "$SHARDS" ]; do listed "$i"; i=$((i + 1)); done)
    printf '%s\n' "$all" | grep -vxF -e "$named" -e "" || true
  fi
}

check() {
  if [ -n "${1:-}" ] && [ "$1" != "$SHARDS" ]; then
    echo "the matrix has $1 shards, this script $SHARDS"
    exit 1
  fi
  if [ -n "${2:-}${3:-}" ]; then
    case "${2:-}:${3:-}" in
      [0-9]:[1-9]) ;;
      *) echo "job index [${2:-}] or shard [${3:-}] is not a number"; exit 1 ;;
    esac
    if [ "$3" -ne $(($2 + 1)) ]; then
      echo "job $(($2 + 1)) of the matrix is shard $3; the matrix must be 1 to $SHARDS in order"
      exit 1
    fi
  fi
  all=$(go list ./... | sort)
  got=$(i=1; while [ "$i" -le "$SHARDS" ]; do shard "$i"; i=$((i + 1)); done | sort)
  twice=$(printf '%s\n' "$got" | uniq -d)
  missing=$(printf '%s\n' "$all" | comm -23 - "$(tmp "$got")")
  unknown=$(printf '%s\n' "$got" | uniq | comm -13 "$(tmp "$all")" -)
  ok=true
  [ -z "$twice" ] || { echo "in more than one shard:"; echo "$twice"; ok=false; }
  [ -z "$missing" ] || { echo "in no shard:"; echo "$missing"; ok=false; }
  [ -z "$unknown" ] || { echo "listed but no package:"; echo "$unknown"; ok=false; }
  $ok || exit 1
  i=1
  while [ "$i" -le "$SHARDS" ]; do
    echo "shard $i: $(shard "$i" | wc -l) packages"
    i=$((i + 1))
  done
  echo "all $(printf '%s\n' "$all" | wc -l) packages in exactly one shard"
}

# comm needs two files; the list goes to a temporary one.
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT
tmp() {
  f=$(mktemp "$tmpdir/list.XXXXXX")
  printf '%s\n' "$1" > "$f"
  echo "$f"
}

case "${1:-}" in
  --check) check "${2:-}" "${3:-}" "${4:-}" ;;
  [1-9])
    [ "$1" -le "$SHARDS" ] || { echo "no shard $1 of $SHARDS" >&2; exit 2; }
    shard "$1"
    ;;
  *) echo "usage: $0 N | --check [TOTAL [INDEX SHARD]]" >&2; exit 2 ;;
esac
