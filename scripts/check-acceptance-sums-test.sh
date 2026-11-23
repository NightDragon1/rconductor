#!/bin/sh
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Checks scripts/check-acceptance-sums.sh (NFR-019): a matching list passes;
# a changed, an extra and a missing file fail.
set -eu
src=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fails=0

check() { # name, expected exit code
  rc=0
  "$src/scripts/check-acceptance-sums.sh" "$tmp/a" >/dev/null 2>&1 || rc=$?
  if [ "$rc" -ne "$2" ]; then
    echo "check-acceptance-sums-test: $1: exit $rc, want $2" >&2
    fails=$((fails + 1))
  fi
}

mkdir "$tmp/a"
printf 'package a\n' >"$tmp/a/a_test.go"
printf 'module a\n' >"$tmp/a/go.mod"
printf '# A\n' >"$tmp/a/README.md"
(cd "$tmp/a" && sha256sum a_test.go go.mod >SHA256SUMS)
check "matching list" 0

printf '// x\n' >>"$tmp/a/a_test.go"
check "changed file" 1
printf 'package a\n' >"$tmp/a/a_test.go"
check "restored file" 0

printf 'package a\n' >"$tmp/a/b_test.go"
check "extra file" 1
rm "$tmp/a/b_test.go"

rm "$tmp/a/go.mod"
check "missing file" 1

if [ "$fails" -ne 0 ]; then
  echo "check-acceptance-sums-test: $fails failed" >&2
  exit 1
fi
echo "check-acceptance-sums-test passed"
