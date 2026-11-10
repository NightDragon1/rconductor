#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Full gate: formatting, vet, lint, SPDX headers, generated code, Go tests
# with the race detector and the web checks. Stops at the first failure.
# Needs Go, Docker (sqlc image) and npm ci in web/. With --no-tests (the
# pre-push hook) the Go and web tests are left out; the CI runs them.
set -euo pipefail
cd "$(dirname "$0")/.."
tests=true
[ "${1:-}" = --no-tests ] && tests=false

step() { printf '== %s\n' "$*"; }

step gofmt
unformatted=$(gofmt -l cmd internal migrations)
if [[ -n "$unformatted" ]]; then
  echo "not formatted:"
  echo "$unformatted"
  exit 1
fi

step go vet
go vet ./...

step golangci-lint
go tool -modfile=tools/go.mod golangci-lint run ./...

step SPDX headers
scripts/check-spdx.sh

step release scripts
scripts/release-test.sh

step CI scripts
scripts/ci-scope-test.sh

step generated code
go generate ./...
docker run --rm -u "$(id -u):$(id -g)" -v "$PWD:/src" -w /src sqlc/sqlc:1.31.1 diff
(cd web && npm run -s gen:api >/dev/null && npx prettier --write src/api/schema.d.ts >/dev/null)
git diff --exit-code -- internal/api web/src/api

if $tests; then
  step go test
  # The race detector needs cgo; binaries stay without it.
  CGO_ENABLED=1 go test -race ./...
fi

step web
(cd web && npm run -s lint && npm run -s format:check)
if $tests; then
  (cd web && npm run -s test)
fi

echo "check passed"
