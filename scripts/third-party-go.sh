#!/bin/sh
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Prints the licenses of the Go code linked into the given commands
# (NFR-021): every module with name, version, license and license text, then
# the Go standard library. Usage: third-party-go.sh <go-licenses> <pkg>...
# go-licenses comes from tools/go.mod (pinned version). Fails if a module
# has no known license or no license text, so that the list stays complete.
set -eu
cd "$(dirname "$0")/.."
bin=$1
shift
report=$("$bin" report --template scripts/third-party-go.tmpl \
  --ignore github.com/NightDragon1/rconductor "$@")
bad=$(printf '%s\n' "$report" | awk '
  /^== / {
    if (head != "" && !text) print head
    if ($0 ~ /\(Unknown\)$/) print
    head = $0; text = 0; next
  }
  NF { text = 1 }
  END { if (head != "" && !text) print head }')
if [ -n "$bad" ]; then
  echo "third-party-go.sh: license missing:" >&2
  printf '%s\n' "$bad" >&2
  exit 1
fi
goversion=$(go env GOVERSION)
goroot=$(go env GOROOT)
printf '%s\n' "$report"
echo "== Go standard library $goversion (BSD-3-Clause)"
cat "$goroot/LICENSE"
