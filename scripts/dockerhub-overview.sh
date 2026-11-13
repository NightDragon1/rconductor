#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Prints the Docker Hub texts of an image repo for a version (PRJ-003): the
# overview from deploy/dockerhub/overview-<panel|agent>.md, or with --short
# the short description (at most 100 characters). A test version 0.x carries
# the pre-release notice and no line "Tags:"; from 1.0 on it is the other way
# round. scripts/release-version.sh decides which a version is. {version}
# becomes the version.
#
# The panel overview takes the security note, the quick start and the
# important variables of README.md in place of its marker lines, with
# relative links made absolute (Docker Hub has no repo to resolve them
# against); the agent has a text of its own. Both stay within the 25,000
# characters Docker Hub allows (PRJ-004).
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

# absolute makes relative links of README.md absolute and drops trailing
# blank lines.
absolute() {
  sed 's#](\([^):]*\))#](https://github.com/NightDragon1/rconductor/blob/main/\1)#g' |
    awk '/^$/ { n++; next } { for (; n > 0; n--) print ""; print }'
}
# section prints the section of README.md with the given heading.
section() {
  awk -v h="## $1" '/^## / { on = ($0 == h) } on' README.md | absolute
}
# security prints the paragraph of README.md that starts with "Run it only".
security() {
  awk '/^Run it only/ { on = 1 } on && /^$/ { exit } on' README.md | absolute
}
overview() {
  if [ "$prerelease" = true ]; then
    # Drop the line of the stable tags and the blank line after it.
    awk '/^Tags: / { tags = 1; next } tags && /^$/ { tags = 0; next } tags && /^[^ ]/ && !/^$/ { next } { tags = 0; print }' "$template"
  else
    # Drop the pre-release blockquote and the blank line after it.
    awk '/^>/ { quote = 1; next } quote && /^$/ { quote = 0; next } { quote = 0; print }' "$template"
  fi | while IFS= read -r line; do
    case $line in
      "<!-- security note of README.md -->") security ;;
      "<!-- quick start of README.md -->") section "Quick start" ;;
      "<!-- important variables of README.md -->") section "Important variables" ;;
      *) printf '%s\n' "$line" | sed "s/{version}/$version/g" ;;
    esac
  done
}
template=deploy/dockerhub/overview-$kind.md
text=$(overview)
case $text in
  *"<!-- "*" of README.md -->"*) echo "a part of README.md was not filled in" >&2; exit 1 ;;
esac
for part in "Quick start" "in your LAN or behind a VPN" "## Links" "https://github.com/NightDragon1/rconductor/wiki"; do
  case $text in
    *"$part"*) ;;
    *) echo "the overview of $kind lacks: $part" >&2; exit 1 ;;
  esac
done
relative=$(printf '%s\n' "$text" | grep -o '](\([^)]*\))' | grep -v '^](https://' || true)
[ -z "$relative" ] || { echo "relative links in the overview: $relative" >&2; exit 1; }
length=$(printf '%s' "$text" | wc -m)
[ "$length" -le 25000 ] || { echo "the overview has $length characters, Docker Hub allows 25000" >&2; exit 1; }
printf '%s\n' "$text"
