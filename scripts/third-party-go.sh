#!/bin/sh
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Prints the licenses of the Go code linked into the given commands
# (NFR-021): every module with name, version, license and license text, then
# the Go standard library. Usage: third-party-go.sh <go-licenses> <pkg>...
# go-licenses comes from tools/go.mod (pinned version).
set -eu
cd "$(dirname "$0")/.."
bin=$1
shift
"$bin" report --template scripts/third-party-go.tmpl \
  --ignore github.com/NightDragon1/rconductor "$@" 2>/dev/null
echo "== Go standard library $(go env GOVERSION) (BSD-3-Clause)"
cat "$(go env GOROOT)/LICENSE"
