#!/bin/bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Measures a real panel within the memory limit of the Compose files, for the
# residual risks of Sicherheit chapter 5. Linux with systemd --user only.
#
#   scripts/measure-panel.sh body 120   R-07: largest bodies with a session
#   scripts/measure-panel.sh hold 180   R-06: places held, reconnecting at once
#
# All data lives in a temporary directory, the panel ends with the script. The
# initial password stays in that directory and is removed with it.
set -eu
cd "$(dirname "$0")/.."
mode=${1:-body}
seconds=${2:-120}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/data" "$work/secrets" "$work/backups"
CGO_ENABLED=0 go build -o "$work/panel" ./cmd/panel
go build -o "$work/load" scripts/loadtest/main.go

export RCD_HTTP_ADDR=127.0.0.1:18080 RCD_AGENT_ADDR=127.0.0.1:18443 RCD_AGENT_PUBLIC_ADDR=127.0.0.1:18443
export RCD_DATA_DIR=$work/data RCD_SECRETS_DIR=$work/secrets RCD_BACKUP_DIR=$work/backups
export RCD_PUBLIC_URL=http://127.0.0.1:18080
systemd-run --user --scope -q -p MemoryMax=1G -p MemorySwapMax=0 "$work/panel" >"$work/out.txt" 2>&1 &
sleep 3
pid=$(pgrep -f "^$work/panel" | head -n 1)
cgroup=/sys/fs/cgroup$(cut -d: -f3 "/proc/$pid/cgroup")
password=$(grep -m 1 'password:' "$work/out.txt" | awk '{print $2}')
cookie=$(curl -si -X POST -H 'Content-Type: application/json' -H 'Origin: http://127.0.0.1:18080' \
  -d "{\"pseudonym\":\"admin\",\"password\":\"$password\"}" http://127.0.0.1:18080/api/v1/auth/login |
  grep -i '^set-cookie' | sed 's/^[^:]*: //; s/;.*//' | tr -d '\r')

"$work/load" -m "$mode" -d "${seconds}s" -c "$cookie"

echo "memory limit: $(($(cat "$cgroup/memory.max") / 1048576)) MiB"
echo "peak: $(($(cat "$cgroup/memory.peak") / 1048576)) MiB, $(grep -E '^oom_kill ' "$cgroup/memory.events")"
echo "audit entries not written: $(grep -c 'audit entry not written' "$work/out.txt" || true)"
kill -0 "$pid" && echo "panel alive"
kill "$pid"
