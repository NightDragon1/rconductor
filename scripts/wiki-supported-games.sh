#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Prints the wiki page Supported games from profiles/*.yaml (DOC-005,
# CAT-001). The profiles stay the only source: the page is made by
# profiles.SupportedGamesPage, which reads them with the panel's own
# parser, and a test in internal/profiles holds it against profiles/.
#
#   scripts/wiki-supported-games.sh > ../rconductor.wiki/Supported-games.md
set -eu
cd "$(dirname "$0")/.."
exec go run scripts/wikigames/main.go
