#!/bin/sh
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# The image build is part of the core (PRJ-005): Dockerfile and .dockerignore
# may change only in a stage-2 pull request that also updates the checksum
# below. Tags and digests of the base images (FROM lines, the image names
# stay) and the syntax line are
# left out of the checksum, so that updates of base images stay stage 1.
# Usage: check-dockerfile.sh [repo root]; prints the checksum with --print.
set -eu
want=eaf22e590411f011361f785be4860ce9f880edd03c5978c6cde74940eb9c466c
print=false
if [ "${1:-}" = --print ]; then print=true; shift; fi
root=${1:-$(dirname "$0")/..}
normalized() {
  sed -E \
    -e 's|^# syntax=.*$|# syntax=|' \
    -e 's|^(FROM( --platform=[^ ]+)? [^ @:]+)(:[^ @]+)?(@sha256:[0-9a-f]+)?|\1|' \
    "$root/Dockerfile"
  echo '--- .dockerignore'
  cat "$root/.dockerignore"
}
got=$(normalized | sha256sum | cut -d' ' -f1)
if $print; then echo "$got"; exit 0; fi
if [ "$got" != "$want" ]; then
  echo "Dockerfile or .dockerignore changed beyond the base images: update the checksum in scripts/check-dockerfile.sh in a stage-2 pull request ($got)" >&2
  exit 1
fi
echo "image build unchanged"
