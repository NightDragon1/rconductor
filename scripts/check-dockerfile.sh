#!/bin/sh
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# The image build is part of the core (PRJ-005): Dockerfile and .dockerignore
# may change only in a pull request that also updates the checksum
# below. Of the base images and the syntax frontend only the version numbers
# in the tag and the digest values are left out of the checksum, so that
# updates of base images stay stage 1; names, tag variants and pinning stay.
# Usage: check-dockerfile.sh [repo root]; prints the checksum with --print.
set -eu
want=2705bdc95828e5db0d24547ccb4538d53af35f67be448125d2ed86165fc28084
print=false
if [ "${1:-}" = --print ]; then print=true; shift; fi
root=${1:-$(dirname "$0")/..}
normalized() {
  # Of a base image or the syntax frontend only the numbers of the tag and
  # the value of the digest are left out: name, the rest of the tag and
  # whether a digest is pinned stay part of the checksum.
  sed -E '/^(FROM|# syntax=)/{s#@sha256:[0-9a-f]{64}#@sha256:#; s#([:.-])[0-9]+#\1N#g}' \
    "$root/Dockerfile"
  echo '--- .dockerignore'
  cat "$root/.dockerignore"
}
# A Dockerfile.dockerignore would take the place of .dockerignore.
for f in "$root"/*.dockerignore; do
  [ -e "$f" ] || continue
  echo "${f#"$root"/} would replace the checked .dockerignore" >&2
  exit 1
done
got=$(normalized | sha256sum | cut -d' ' -f1)
if $print; then echo "$got"; exit 0; fi
if [ "$got" != "$want" ]; then
  echo "Dockerfile or .dockerignore changed beyond the base images: update the checksum in scripts/check-dockerfile.sh ($got)" >&2
  exit 1
fi
echo "image build unchanged"
