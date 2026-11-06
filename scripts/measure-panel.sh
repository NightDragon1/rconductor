#!/bin/bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Measures a real panel within the memory limit of the Compose files, for the
# residual risks of Sicherheit chapter 5. Linux with systemd --user only.
#
#   scripts/measure-panel.sh body 120     R-07: largest bodies of a supporter
#   scripts/measure-panel.sh picture 120  R-07: costliest pictures of a supporter
#   scripts/measure-panel.sh read 60      R-07: parallel reads of a picture
#   scripts/measure-panel.sh all 180      R-07: all of it with password checks
#   scripts/measure-panel.sh hold 180     R-06: places held, reconnecting at once
#
# GOMEMLIMIT=off in the environment measures without the Go memory limit.
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

# A session of the role with the fewest rights: the admin changes the initial
# password and invites a supporter. The rights are checked after decoding.
base=http://127.0.0.1:18080/api/v1
post() { curl -si -X POST -H 'Content-Type: application/json' -H 'Origin: http://127.0.0.1:18080' ${3:+-H "Cookie: $3"} -d "$2" "$base$1"; }
session() { grep -i '^set-cookie' | sed 's/^[^:]*: //; s/;.*//' | tr -d '\r' | head -n 1; }
initial=$(grep -m 1 'password:' "$work/out.txt" | awk '{print $2}')
pass=measure-panel-$$-password
admin=$(post /auth/login "{\"pseudonym\":\"admin\",\"password\":\"$initial\"}" | session)
post /me/password "{\"current_password\":\"$initial\",\"new_password\":\"$pass\"}" "$admin" >/dev/null
admin=$(post /auth/login "{\"pseudonym\":\"admin\",\"password\":\"$pass\"}" | session)
token=$(post /invites '{"role":"supporter"}' "$admin" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
post /invites/redeem "{\"token\":\"$token\",\"pseudonym\":\"low\",\"password\":\"$pass\"}" >/dev/null
cookie=$(post /auth/login "{\"pseudonym\":\"low\",\"password\":\"$pass\"}" | session)
[ -n "$cookie" ] || { echo "no session of a supporter" >&2; kill "$pid"; exit 1; }

# A picture of about 1 MB to read, under the session of the supporter.
head -c 700000 /dev/urandom | python3 -c '
import struct, sys, zlib
raw = sys.stdin.buffer.read()
w = h = 480
rows = b"".join(b"\0" + raw[y * w * 3:(y + 1) * w * 3] for y in range(h))
def chunk(t, d):
    return struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d))
sys.stdout.buffer.write(b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(rows, 1)) + chunk(b"IEND", b""))
' >"$work/picture.png"
curl -s -o /dev/null -X PUT -H 'Content-Type: image/png' -H 'Origin: http://127.0.0.1:18080' -H "Cookie: $cookie" --data-binary "@$work/picture.png" "$base/me/avatar"
id=$(curl -s -H "Cookie: $cookie" "$base/auth/session" | grep -o '"id":"[^"]*"' | head -n 1 | cut -d'"' -f4)

"$work/load" -m "$mode" -d "${seconds}s" -c "$cookie" -p "/api/v1/users/$id/avatar"

grep -m 1 'memory limit' "$work/out.txt" | cut -c1-160 || true
echo "audit entries not written: $(grep -c 'audit entry not written' "$work/out.txt" || true)"
if ! kill -0 "$pid" 2>/dev/null; then
  echo "panel killed"
  exit 1
fi
echo "memory limit: $(($(cat "$cgroup/memory.max") / 1048576)) MiB"
echo "peak: $(($(cat "$cgroup/memory.peak") / 1048576)) MiB, $(grep -E '^oom_kill ' "$cgroup/memory.events")"
echo "panel alive"
kill "$pid"
