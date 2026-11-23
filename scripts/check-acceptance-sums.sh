#!/bin/sh
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Fails when acceptance/ and acceptance/SHA256SUMS differ: every file in
# acceptance/ except README.md and SHA256SUMS is listed with its SHA-256,
# no file is missing and none is extra (NFR-019).
#   check-acceptance-sums.sh [DIR]     (default: acceptance)
set -eu
dir=${1:-acceptance}
cd "$dir"
listed=$(awk '{sub(/^\*/, "", $2); print $2}' SHA256SUMS | sort)
present=$(find . -type f ! -name SHA256SUMS ! -name README.md | sed 's|^\./||' | sort)
if [ "$listed" != "$present" ]; then
  echo "check-acceptance-sums: $dir and $dir/SHA256SUMS list different files" >&2
  printf 'listed:\n%s\npresent:\n%s\n' "$listed" "$present" >&2
  exit 1
fi
sha256sum --strict --quiet -c SHA256SUMS
echo "check-acceptance-sums: $(printf '%s\n' "$listed" | wc -l) files match"
