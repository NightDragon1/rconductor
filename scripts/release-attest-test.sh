#!/bin/sh
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Tests of scripts/release-attest.sh (SEC-013) against stand-ins for cosign,
# syft and release-digests.sh: a network error of cosign runs again and then
# passes; network errors beyond SIGN_ATTEMPTS fail; any other error fails at
# once; bad settings are refused.
set -eu
src=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fails=0

mkdir -p "$tmp/work/scripts" "$tmp/bin"
cp "$src/scripts/release-attest.sh" "$tmp/work/scripts/"
cat >"$tmp/work/scripts/release-digests.sh" <<'STUB'
#!/bin/sh
printf 'amd64 sha256:a\narm64 sha256:b\n'
STUB
# cosign fails FAIL_TIMES times with FAIL_MSG, then passes; counts all calls.
cat >"$tmp/bin/cosign" <<'STUB'
#!/bin/sh
n=$(cat "$STATE/count" 2>/dev/null || echo 0)
n=$((n + 1))
echo "$n" >"$STATE/count"
if [ "$n" -le "$FAIL_TIMES" ]; then echo "$FAIL_MSG" >&2; exit 1; fi
exit 0
STUB
cat >"$tmp/bin/syft" <<'STUB'
#!/bin/sh
exit 0
STUB
chmod +x "$tmp/work/scripts/"*.sh "$tmp/bin/cosign" "$tmp/bin/syft"

# run NAME WANT_EXIT WANT_CALLS FAIL_TIMES FAIL_MSG [ATTEMPTS]
run() {
  rm -f "$tmp/count"
  rc=0
  (cd "$tmp/work" && STATE=$tmp FAIL_TIMES=$4 FAIL_MSG=$5 SIGN_ATTEMPTS=${6:-4} SIGN_DELAY=0 \
    GHCR=ghcr.io/x HUB=docker.io/x PATH="$tmp/bin:$PATH" \
    scripts/release-attest.sh panel sha256:i >/dev/null 2>&1) || rc=$?
  calls=$(cat "$tmp/count" 2>/dev/null || echo 0)
  if [ "$rc" -ne "$2" ] || [ "$calls" -ne "$3" ]; then
    echo "release-attest-test: $1: exit $rc with $calls cosign calls, want $2 with $3" >&2
    fails=$((fails + 1))
  fi
}

# 2 index signatures + 2 platforms x 2 registries x 3 calls = 14 calls
run "no error" 0 14 0 ""
run "network error twice, then passes" 0 16 2 "dial tcp: connection reset by peer"
run "server error passes on retry" 0 15 1 "POST https://rekor: 503 Service Unavailable"
run "network errors beyond the attempts" 1 4 9 "read: connection reset by peer"
run "one attempt only" 1 1 1 "i/o timeout" 1
run "other error fails at once" 1 1 1 "signing: permission denied"

rc=0
(cd "$tmp/work" && SIGN_ATTEMPTS=0 GHCR=g HUB=h scripts/release-attest.sh panel sha256:i >/dev/null 2>&1) || rc=$?
[ "$rc" -eq 2 ] || { echo "release-attest-test: SIGN_ATTEMPTS=0 exit $rc, want 2" >&2; fails=$((fails + 1)); }
rc=0
(cd "$tmp/work" && SIGN_DELAY=x GHCR=g HUB=h scripts/release-attest.sh panel sha256:i >/dev/null 2>&1) || rc=$?
[ "$rc" -eq 2 ] || { echo "release-attest-test: SIGN_DELAY=x exit $rc, want 2" >&2; fails=$((fails + 1)); }

if [ "$fails" -ne 0 ]; then
  echo "release-attest-test: $fails failed" >&2
  exit 1
fi
echo "release-attest-test passed"
