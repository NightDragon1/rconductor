#!/bin/sh
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Tests of check-dockerfile.sh (PRJ-005) on copies of the image build.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
root=$here/..
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fail=0
copy() { mkdir -p "$tmp/$1"; cp "$root/Dockerfile" "$root/.dockerignore" "$tmp/$1/"; }
check() { # check <label> <want exit> <dir>
  if sh "$here/check-dockerfile.sh" "$3" >/dev/null 2>&1; then got=0; else got=1; fi
  if [ "$got" != "$2" ]; then echo "FAIL $1: exit $got, want $2"; fail=1; fi
}
copy same
check "unchanged" 0 "$tmp/same"
copy digest
sed -i -E 's|@sha256:[0-9a-f]+|@sha256:0000000000000000000000000000000000000000000000000000000000000000|; s|node:[^@ ]+|node:27-trixie-slim|' "$tmp/digest/Dockerfile"
check "new base image tag and digest" 0 "$tmp/digest"
copy name
sed -i -E '0,/^FROM/s|node:|evil/node:|' "$tmp/name/Dockerfile"
check "other base image" 1 "$tmp/name"
copy run
printf 'RUN npm run build\n' >>"$tmp/run/Dockerfile"
check "another build step" 1 "$tmp/run"
copy ignore
printf 'scripts\n' >>"$tmp/ignore/.dockerignore"
check "changed .dockerignore" 1 "$tmp/ignore"
[ "$fail" = 0 ] && echo "check-dockerfile-test passed"
exit "$fail"
