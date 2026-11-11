#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Turns a release tag into what the release needs, as GITHUB_OUTPUT lines
# (D-047): v1.0 gives the image tags 1.0 and 1. A test version v0.9 gives only
# 0.9 and is a pre-release, so it never moves a tag people follow. A release
# candidate v1.1-rc1 gives only 1.1-rc1, is a pre-release and leaves the
# Docker Hub description alone (rc=true).
set -eu
[ $# -eq 1 ] || { echo "usage: release-version.sh TAG" >&2; exit 2; }
printf '%s\n' "$1" | grep -qE '^v[0-9]+\.[0-9]+(-rc[1-9][0-9]*)?$' || {
  echo "not a release tag: $1 (want v1.0, v1.1-rc1, or v0.9 for a test version)" >&2
  exit 1
}
version=${1#v}
major=${version%%.*}
rc=false
case $version in
  *-rc*)
    tags=$version
    prerelease=true
    rc=true
    ;;
  *)
    if [ "$major" = 0 ]; then
      tags=$version
      prerelease=true
    else
      tags="$version $major"
      prerelease=false
    fi
    ;;
esac
echo "version=$version"
echo "tags=$tags"
echo "prerelease=$prerelease"
echo "rc=$rc"
