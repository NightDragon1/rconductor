#!/bin/sh
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Checks scripts/third-party-go.sh (NFR-021) with stub reports in place of
# go-licenses: a complete list passes and ends with the Go standard library;
# an unknown license, a missing license text or a failing tool fails.
set -eu
cd "$(dirname "$0")/.."
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
fails=0
stub() { # name, report
  printf '#!/bin/sh\nprintf "%s"\n' "$2" > "$dir/$1"
  chmod +x "$dir/$1"
}
stub ok '== a v1 (MIT)\nMIT text\n== b v2 (BSD-3-Clause)\nBSD text\n'
stub unknown '== a v1 (MIT)\nMIT text\n== b v2 (Unknown)\ntext\n'
stub empty '== a v1 (MIT)\n\n== b v2 (MIT)\nMIT text\n'
stub lastempty '== a v1 (MIT)\nMIT text\n== b v2 (MIT)\n'
printf '#!/bin/sh\nexit 3\n' > "$dir/broken"
chmod +x "$dir/broken"

out=$(scripts/third-party-go.sh "$dir/ok" ./cmd/agent) || { echo "ok: failed" >&2; fails=$((fails + 1)); }
printf '%s\n' "$out" | grep -q '^== b v2 (BSD-3-Clause)$' || { echo "ok: module missing" >&2; fails=$((fails + 1)); }
printf '%s\n' "$out" | grep -q '^== Go standard library go.* (BSD-3-Clause)$' || { echo "ok: standard library missing" >&2; fails=$((fails + 1)); }
for s in unknown empty lastempty broken; do
  if scripts/third-party-go.sh "$dir/$s" ./cmd/agent > /dev/null 2>&1; then
    echo "$s: passed" >&2
    fails=$((fails + 1))
  fi
done
[ "$fails" -eq 0 ] || exit 1
echo "third-party-go ok"
