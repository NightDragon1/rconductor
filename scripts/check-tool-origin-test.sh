#!/bin/sh
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Tests of check-tool-origin.sh (PRJ-005) on temporary trees.
set -eu
script=$(cd "$(dirname "$0")" && pwd)/check-tool-origin.sh
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fail=0
tree() { # tree <dir> <resolved of eslint> <go.mod line>
  mkdir -p "$1/web" "$1/tools"
  pkgs=""
  for n in typescript openapi-typescript prettier vitest vite @playwright/test; do
    pkgs="$pkgs\"node_modules/$n\": {\"resolved\": \"https://registry.npmjs.org/$n/-/x.tgz\", \"integrity\": \"sha512-x\"},"
  done
  printf '{"packages": {%s "node_modules/eslint": {"resolved": "%s", "integrity": "sha512-x"}}}\n' "$pkgs" "$2" >"$1/web/package-lock.json"
  printf 'module x\n\ngo 1.27\n%s\n' "$3" >"$1/tools/go.mod"
}
check() { # check <label> <want exit> <dir>
  if sh "$script" "$3" >/dev/null 2>&1; then got=0; else got=1; fi
  if [ "$got" != "$2" ]; then echo "FAIL $1: exit $got, want $2"; fail=1; fi
}
tree "$tmp/ok" https://registry.npmjs.org/eslint/-/eslint-9.tgz ""
check "all from the registry" 0 "$tmp/ok"
tree "$tmp/file" file:../eslint ""
check "eslint from a file" 1 "$tmp/file"
tree "$tmp/git" git+https://example.invalid/eslint.git ""
check "eslint from git" 1 "$tmp/git"
tree "$tmp/replace" https://registry.npmjs.org/eslint/-/eslint-9.tgz "replace golang.org/x/tools => ../x"
check "replace in tools/go.mod" 1 "$tmp/replace"
tree "$tmp/block" https://registry.npmjs.org/eslint/-/eslint-9.tgz "replace (
	golang.org/x/tools => ../x
)"
check "replace block in tools/go.mod" 1 "$tmp/block"
[ "$fail" = 0 ] && echo "check-tool-origin-test passed"
exit "$fail"
