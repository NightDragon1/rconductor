#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Pulls the foreign images of the tests in the given directories, so
# testcontainers finds them locally. It discards the pull stream, a
# failed download only shows later as "No such image" (#106); here the
# registry's error reaches the log and a failed pull is tried again.
#
# The images come from the tests themselves: every reference pinned by
# digest in a *_test.go file. Ryuk has no digest here, testcontainers
# looks it up under the tag of its module.
set -eu
[ $# -gt 0 ] || { echo "usage: pull-test-images.sh DIR..." >&2; exit 2; }
ATTEMPTS=${PULL_ATTEMPTS:-5}
DELAY=${PULL_DELAY:-10}

images=$(grep -rhoE --include='*_test.go' '"[^" ]+@sha256:[0-9a-f]{64}"' "$@" | tr -d '"' | sort -u)
[ -n "$images" ] || { echo "no pinned images in: $*" >&2; exit 1; }
module=$(go list -m -f '{{.Dir}}' github.com/testcontainers/testcontainers-go)
ryuk=$(sed -n 's/^const ReaperDefaultImage = "\(.*\)"$/\1/p' "$module/internal/config/config.go")
[ -n "$ryuk" ] || { echo "no Ryuk image in $module" >&2; exit 1; }

for image in $images $ryuk; do
  attempt=1
  until docker pull --quiet "$image"; do
    if [ "$attempt" -ge "$ATTEMPTS" ]; then
      echo "giving up on $image after $attempt attempts" >&2
      exit 1
    fi
    echo "pull of $image failed (attempt $attempt of $ATTEMPTS), trying again" >&2
    sleep $((DELAY * attempt))
    attempt=$((attempt + 1))
  done
done
