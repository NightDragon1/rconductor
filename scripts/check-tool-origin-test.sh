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
    pkgs="$pkgs\"node_modules/$n\": {\"resolved\": \"https://registry.npmjs.org/$n/-/${n##*/}-1.0.0.tgz\", \"integrity\": \"sha512-x\"},"
  done
  printf '{"packages": {%s "node_modules/eslint": {"resolved": "%s", "integrity": "sha512-x"}}}\n' "$pkgs" "$2" >"$1/web/package-lock.json"
  printf 'module x\n\ngo 1.27\n%s\n' "$3" >"$1/tools/go.mod"
  printf 'module y\n\ngo 1.27\n' >"$1/go.mod"
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
tree "$tmp/dep" https://registry.npmjs.org/eslint/-/eslint-9.tgz ""
node -e 'const f=process.argv[1],l=JSON.parse(require("fs").readFileSync(f));l.packages["node_modules/eslint-plugin-x"]={resolved:"file:../x",integrity:"sha512-x"};require("fs").writeFileSync(f,JSON.stringify(l))' "$tmp/dep/web/package-lock.json"
check "a dependency from a file" 1 "$tmp/dep"
tree "$tmp/link" https://registry.npmjs.org/eslint/-/eslint-9.tgz ""
node -e 'const f=process.argv[1],l=JSON.parse(require("fs").readFileSync(f));l.packages["node_modules/vite"].link=true;require("fs").writeFileSync(f,JSON.stringify(l))' "$tmp/link/web/package-lock.json"
check "a linked tool" 1 "$tmp/link"
tree "$tmp/nosum" https://registry.npmjs.org/eslint/-/eslint-9.tgz ""
node -e 'const f=process.argv[1],l=JSON.parse(require("fs").readFileSync(f));delete l.packages["node_modules/vitest"].integrity;require("fs").writeFileSync(f,JSON.stringify(l))' "$tmp/nosum/web/package-lock.json"
check "a tool without checksum" 1 "$tmp/nosum"
tree "$tmp/missing" https://registry.npmjs.org/eslint/-/eslint-9.tgz ""
node -e 'const f=process.argv[1],l=JSON.parse(require("fs").readFileSync(f));delete l.packages["node_modules/prettier"];require("fs").writeFileSync(f,JSON.stringify(l))' "$tmp/missing/web/package-lock.json"
check "a tool missing" 1 "$tmp/missing"
tree "$tmp/bundled" https://registry.npmjs.org/eslint/-/eslint-9.tgz ""
node -e 'const f=process.argv[1],l=JSON.parse(require("fs").readFileSync(f));l.packages["node_modules/vite/node_modules/b"]={inBundle:true};require("fs").writeFileSync(f,JSON.stringify(l))' "$tmp/bundled/web/package-lock.json"
check "a bundled package" 0 "$tmp/bundled"
tree "$tmp/rootreplace" https://registry.npmjs.org/eslint/-/eslint-9.tgz ""
printf 'replace a => ../a\n' >>"$tmp/rootreplace/go.mod"
check "replace in go.mod" 1 "$tmp/rootreplace"
tree "$tmp/work" https://registry.npmjs.org/eslint/-/eslint-9.tgz ""
printf 'go 1.27\nuse .\n' >"$tmp/work/tools/go.work"
check "tools/go.work" 1 "$tmp/work"
tree "$tmp/alias" https://registry.npmjs.org/eslint/-/eslint-9.tgz ""
node -e 'const f=process.argv[1],l=JSON.parse(require("fs").readFileSync(f));l.packages["node_modules/vite"].resolved="https://registry.npmjs.org/evil-vite/-/evil-vite-1.0.0.tgz";require("fs").writeFileSync(f,JSON.stringify(l))' "$tmp/alias/web/package-lock.json"
check "a tool as another package (alias)" 1 "$tmp/alias"
tree "$tmp/dots" https://registry.npmjs.org/eslint/-/eslint-9.tgz ""
node -e 'const f=process.argv[1],l=JSON.parse(require("fs").readFileSync(f));l.packages["node_modules/vite"].resolved="https://registry.npmjs.org/vite/-/vite-/../../../evil/-/evil-1.0.0.tgz";require("fs").writeFileSync(f,JSON.stringify(l))' "$tmp/dots/web/package-lock.json"
check "a tool behind dot segments" 1 "$tmp/dots"
[ "$fail" = 0 ] && echo "check-tool-origin-test passed"
exit "$fail"
