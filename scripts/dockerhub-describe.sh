#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Sets short description and overview of both Docker Hub repos for a version
# (PRJ-003, PRJ-004), with the texts of scripts/dockerhub-overview.sh. Used by
# the release run and by the workflow that only updates the descriptions.
# Needs HUB (docker.io/<namespace>), DOCKERHUB_USERNAME and DOCKERHUB_TOKEN;
# login and bearer token go through stdin and private files only, never
# through arguments or the log. All texts are made before Docker Hub hears of
# anything: an error or an empty text ends the run before the API is called.
set -euo pipefail
cd "$(dirname "$0")/.."
[ $# -eq 1 ] || { echo "usage: dockerhub-describe.sh VERSION" >&2; exit 2; }
version=$1
fail() {
  echo "::error::Docker Hub description update failed (HTTP $1)"
  exit 1
}
declare -A short full
for kind in panel agent; do
  short[$kind]=$(scripts/dockerhub-overview.sh --short "$version" "$kind")
  full[$kind]=$(scripts/dockerhub-overview.sh "$version" "$kind")
  if [ -z "${short[$kind]}" ] || [ -z "${full[$kind]}" ]; then
    echo "::error::empty Docker Hub description for $kind"
    exit 1
  fi
done
umask 077
auth=$(mktemp)
login=$(mktemp)
trap 'rm -f "$auth" "$login"' EXIT
code=$(jq -n '{identifier: env.DOCKERHUB_USERNAME, secret: env.DOCKERHUB_TOKEN}' |
  curl -sS -o "$login" -w '%{http_code}' --data @- -H 'Content-Type: application/json' \
    https://hub.docker.com/v2/auth/token) || code=000
[ "$code" = 200 ] || fail "$code"
jq -er '"Authorization: Bearer " + .access_token' "$login" > "$auth" || fail "$code"
namespace=${HUB#docker.io/}
for kind in panel agent; do
  repo=$namespace/rconductor-$kind
  code=$(jq -n --arg short "${short[$kind]}" --arg full "${full[$kind]}" \
    '{description: $short, full_description: $full}' |
    curl -sS -o /dev/null -w '%{http_code}' -X PATCH --data @- -H 'Content-Type: application/json' -H @"$auth" \
      "https://hub.docker.com/v2/repositories/$repo/") || code=000
  case $code in 2??) ;; *) fail "$code" ;; esac
  echo "updated the description of $repo"
done
