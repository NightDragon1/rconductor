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
copy frontend
sed -i -E '1s|^# syntax=[^@]+|# syntax=evil/frontend:1|' "$tmp/frontend/Dockerfile"
check "other syntax frontend" 1 "$tmp/frontend"
copy frontendbump
sed -i -E '1s|dockerfile:1@sha256:[0-9a-f]+|dockerfile:2@sha256:1111111111111111111111111111111111111111111111111111111111111111|' "$tmp/frontendbump/Dockerfile"
check "new tag and digest of the frontend" 0 "$tmp/frontendbump"
copy variant
sed -i 's|static-debian13:nonroot|static-debian13:debug-nonroot|' "$tmp/variant/Dockerfile"
check "other tag variant" 1 "$tmp/variant"
copy unpinned
sed -i -E '0,/^FROM/s|@sha256:[0-9a-f]+||' "$tmp/unpinned/Dockerfile"
check "digest pin removed" 1 "$tmp/unpinned"
copy scripts
sed -i 's|npm ci --no-audit --no-fund --ignore-scripts|npm ci --no-audit --no-fund|' "$tmp/scripts/Dockerfile"
check "lifecycle scripts back on" 1 "$tmp/scripts"
copy arg
sed -i '0,/^ARG /s|^ARG |ARG EXTRA=1\nARG |' "$tmp/arg/Dockerfile"
check "another ARG" 1 "$tmp/arg"
copy own
touch "$tmp/own/Dockerfile.dockerignore"
check "Dockerfile.dockerignore" 1 "$tmp/own"
[ "$fail" = 0 ] && echo "check-dockerfile-test passed"
exit "$fail"
