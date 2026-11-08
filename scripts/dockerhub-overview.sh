#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Prints the Docker Hub texts of an image repo for a version (PRJ-003): the
# overview from deploy/dockerhub/overview.md, or with --short the short
# description (at most 100 characters). A test version 0.x carries the
# pre-release notice and no line "Tags:"; from 1.0 on it is the other way
# round. scripts/release-version.sh decides which a version is.
#
# The overview takes the quick start of README.md in place of its marker
# line, with relative links made absolute (Docker Hub has no repo to resolve
# them against), and stays within the 25,000 characters Docker Hub allows
# (PRJ-004).
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
  exit 0
fi

marker="<!-- quick start of README.md -->"
# The section "Quick start" of README.md, without trailing blank lines.
quickstart() {
  awk '/^## / { on = ($0 == "## Quick start") } on' README.md |
    sed 's#](\([^):]*\))#](https://github.com/NightDragon1/rconductor/blob/main/\1)#g' |
    awk '/^$/ { n++; next } { for (; n > 0; n--) print ""; print }'
}
overview() {
  if [ "$prerelease" = true ]; then
    # Drop the line of the stable tags and the blank line after it.
    awk '/^Tags: / { tags = 1; next } tags && /^$/ { tags = 0; next } { tags = 0; print }' deploy/dockerhub/overview.md
  else
    # Drop the pre-release blockquote and the blank line after it.
    awk '/^>/ { quote = 1; next } quote && /^$/ { quote = 0; next } { quote = 0; print }' deploy/dockerhub/overview.md
  fi | while IFS= read -r line; do
    if [ "$line" = "$marker" ]; then
      quickstart
    else
      printf '%s\n' "$line"
    fi
  done
}
text=$(overview)
case $text in
  *"$marker"*) echo "the quick start was not filled in" >&2; exit 1 ;;
esac
relative=$(printf '%s\n' "$text" | grep -o '](\([^)]*\))' | grep -v '^](https://' || true)
[ -z "$relative" ] || { echo "relative links in the overview: $relative" >&2; exit 1; }
length=$(printf '%s' "$text" | wc -m)
[ "$length" -le 25000 ] || { echo "the overview has $length characters, Docker Hub allows 25000" >&2; exit 1; }
printf '%s\n' "$text"
