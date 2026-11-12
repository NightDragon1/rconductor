#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Decides how much the git hooks check (NFR-013): "wip" for work in
# progress on a branch wip/*, where the tests are left out and formatting
# and the secret check still run; "full" for everything else.
#   hook-scope.sh commit          the branch checked out now
#   hook-scope.sh push < refs     the lines git gives the pre-push hook:
#                                 wip only if every pushed ref is a wip/* branch
set -euo pipefail
case "${1:-}" in
  commit)
    branch=$(git symbolic-ref -q --short HEAD || true)
    [[ "$branch" == wip/* ]] && echo wip || echo full
    ;;
  push)
    scope=
    while read -r _local_ref _local_sha remote_ref _remote_sha; do
      if [[ "$remote_ref" == refs/heads/wip/* ]]; then
        scope=${scope:-wip}
      else
        scope=full
      fi
    done
    echo "${scope:-full}"
    ;;
  *)
    echo "usage: hook-scope.sh commit|push" >&2
    exit 2
    ;;
esac
