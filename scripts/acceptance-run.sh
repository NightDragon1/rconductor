#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Runs the black-box tests in acceptance/ against a running instance
# (NFR-019): checks acceptance/SHA256SUMS, starts panel and agent from the
# images rconductor-panel:dev and rconductor-agent:dev with
# deploy/compose/all-in-one.yaml, signs in with the initial password from
# the panel log and runs go test in acceptance/. Stops the stack at the end.
#   acceptance-run.sh [go test flags]
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
scripts/check-acceptance-sums.sh

project=rcd-acceptance
compose=(docker compose -p "$project" -f deploy/compose/all-in-one.yaml)
export RCD_IMAGE_REPO=acceptance.local RCD_VERSION=acceptance RCD_WEB_PORT=${RCD_WEB_PORT:-18080}
DOCKER_GID=$(stat -c %g /var/run/docker.sock)
export DOCKER_GID

docker tag rconductor-panel:dev "$RCD_IMAGE_REPO/rconductor-panel:$RCD_VERSION"
docker tag rconductor-agent:dev "$RCD_IMAGE_REPO/rconductor-agent:$RCD_VERSION"
trap '"${compose[@]}" down -v >/dev/null 2>&1 || true' EXIT
"${compose[@]}" up -d --pull never

ready=
for _ in $(seq 1 60); do
  if "${compose[@]}" logs panel 2>/dev/null | grep -q 'event=app.ready'; then ready=1; break; fi
  sleep 2
done
[ -n "$ready" ] || { "${compose[@]}" logs panel >&2; echo "acceptance-run: panel not ready after 120 s" >&2; exit 1; }

pw=$("${compose[@]}" logs panel 2>/dev/null | sed -n 's/.*password: \([^ "]*\).*/\1/p' | head -n 1)
[ -n "$pw" ] || { echo "acceptance-run: no initial password in the panel log" >&2; exit 1; }
[ -z "${GITHUB_ACTIONS:-}" ] || echo "::add-mask::$pw"

cd acceptance
RCD_ACCEPTANCE_URL=http://localhost:$RCD_WEB_PORT RCD_ACCEPTANCE_ADMIN_PASSWORD=$pw \
  go test -count=1 "$@" ./...
