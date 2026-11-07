#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Prints the Docker Hub texts of an image repo for a version (PRJ-003): the
# overview from deploy/dockerhub/overview.md, or with --short the short
# description (at most 100 characters). A test version 0.x carries the
# pre-release notice; from 1.0 on it is left out. scripts/release-version.sh
# decides which a version is.
set -eu
cd "$(dirname "$0")/.."
short=false
if [ "${1:-}" = --short ]; then
  short=true
  shift
fi
[ $# -eq 2 ] || { echo "usage: dockerhub-overview.sh [--short] VERSION panel|agent" >&2; exit 2; }
version=$1
kind=$2
case $kind in
  panel) what="web panel for RCON game servers" ;;
  agent) what="runs game containers for the panel" ;;
  *) echo "unknown repo kind: $kind (want panel or agent)" >&2; exit 2 ;;
esac
facts=$(scripts/release-version.sh "v$version")
prerelease=$(printf '%s\n' "$facts" | sed -n 's/^prerelease=//p')
if [ "$short" = true ]; then
  text="RCONductor $kind – $what."
  [ "$prerelease" = true ] && text="$text Pre-release (0.x): not for production use."
  printf '%s\n' "$text"
elif [ "$prerelease" = true ]; then
  cat deploy/dockerhub/overview.md
else
  # Drop the pre-release blockquote and the blank line after it.
  awk '/^>/ { quote = 1; next } quote && /^$/ { quote = 0; next } { quote = 0; print }' deploy/dockerhub/overview.md
fi
