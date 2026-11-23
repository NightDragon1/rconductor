#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# The job Line limit (PRJ-005): line-limit.sh SIZE LINES CODE TESTS [SUMMARY]
# writes the counts to SUMMARY (the step summary) and warns about an
# overrun; it fails only when the count is unknown (SIZE empty: Classify did
# not finish), never within.
set -euo pipefail
size=${1-} lines=${2-} code=${3-} tests=${4-} summary=${5:-/dev/null}
if [[ -z $size ]]; then
  echo "::error::line count unknown: Classify did not finish"
  exit 1
fi
echo "changed lines: $lines in all, $code without tests ($tests in tests), limit 400 for stage 2" >>"$summary"
case "$size" in
  over) echo "::warning::stage 2 pull request with $code changed lines of code (limit 400): split it or ask first (label over-limit)" ;;
  over-tests) echo "::warning::stage 2 pull request over 400 changed lines through tests alone ($tests in tests; label over-limit-tests)" ;;
  within) echo "within the line limit" ;;
  *) echo "::error::unknown size: $size"; exit 1 ;;
esac
