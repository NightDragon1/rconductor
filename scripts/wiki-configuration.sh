#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Prints the wiki page Configuration from docs/user/configuration.md
# (DOC-001). The guide in the repo stays the only source: tests in
# internal/config, internal/users and internal/app hold its defaults and
# limits against the code. The wiki shows the page name as title, so the
# first heading goes; links into the repo become absolute.
#
#   scripts/wiki-configuration.sh > ../rconductor.wiki/Configuration.md
set -eu
cd "$(dirname "$0")/.."
repo=https://github.com/NightDragon1/rconductor/blob/main
awk 'NR == 1 && /^# / { skip = 1; next } skip && /^$/ { skip = 0; next } { skip = 0; print }' docs/user/configuration.md |
  sed -e "s#](\.\./\.\./#](${repo}/#g" -e "s#](\([a-z-]*\.md\)#](${repo}/docs/user/\1#g"
