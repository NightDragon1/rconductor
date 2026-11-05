#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Turns a release tag into what the release needs, as GITHUB_OUTPUT lines
# (D-047): v1.0 gives the image tags 1.0 and 1, v1.0-rc1 only 1.0-rc1, so a
# release candidate never moves the tags people use.
set -eu
[ $# -eq 1 ] || { echo "usage: release-version.sh TAG" >&2; exit 2; }
version=${1#v}
if printf '%s\n' "$1" | grep -qE '^v[0-9]+\.[0-9]+$'; then
  tags="$version ${version%%.*}"
  prerelease=false
elif printf '%s\n' "$1" | grep -qE '^v[0-9]+\.[0-9]+-rc[0-9]+$'; then
  tags=$version
  prerelease=true
else
  echo "not a release tag: $1 (want v1.0 or v1.0-rc1)" >&2
  exit 1
fi
echo "version=$version"
echo "tags=$tags"
echo "prerelease=$prerelease"
