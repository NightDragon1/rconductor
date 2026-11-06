#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Prints architecture and digest of each platform of a multi-arch image, one
# per line. The attestation manifests of BuildKit have no architecture.
set -eu
[ $# -eq 1 ] || { echo "usage: release-digests.sh IMAGE" >&2; exit 2; }
docker buildx imagetools inspect --raw "$1" |
  jq -r '.manifests[] | select(.platform.os == "linux") | "\(.platform.architecture) \(.digest)"'
