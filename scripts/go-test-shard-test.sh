#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Checks the shard gate of scripts/go-test-shard.sh (NFR-013): a matrix
# passes only if it is exactly 1 to N, checked job by job as in the CI.
# GO_TEST_SHARD names another version of the script to check.
set -eu
cd "$(dirname "$0")/.."
shard=${GO_TEST_SHARD:-scripts/go-test-shard.sh}
fails=0

# matrix VALUES...: runs the check of every job, prints ok or failed.
matrix() {
  total=$#
  index=0
  result=ok
  for value in "$@"; do
    sh "$shard" --check "$total" "$index" "$value" >/dev/null 2>&1 || result=failed
    index=$((index + 1))
  done
  echo "$result"
}
expect() { # what, want, got
  [ "$2" = "$3" ] || { echo "$1: want [$2], got [$3]" >&2; fails=$((fails + 1)); }
}

expect "matrix 1 2 3" ok "$(matrix 1 2 3)"
expect "C32: matrix 1 1 3" failed "$(matrix 1 1 3)"
expect "C32: matrix 2 1 3" failed "$(matrix 2 1 3)"
expect "C32: matrix 1 2 4" failed "$(matrix 1 2 4)"
expect "matrix 1 2" failed "$(matrix 1 2)"

[ "$fails" -eq 0 ] || exit 1
echo "go test shards ok"
