#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Prints the image names of a target for all tags in both registries as a
# multi-line GITHUB_OUTPUT value named images.
set -eu
[ $# -eq 2 ] || { echo "usage: release-images.sh TARGET 'TAG...'" >&2; exit 2; }
target=$1
tags=$2
echo 'images<<EOF'
for registry in "$GHCR" "$HUB"; do
  for tag in $tags; do
    echo "$registry/rconductor-$target:$tag"
  done
done
echo EOF
