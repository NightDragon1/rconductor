#!/bin/sh
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Origin of the tools the core checks run (PRJ-005): every package in
# web/package-lock.json comes from registry.npmjs.org with a checksum, not
# from a file, a link or git; go.mod and tools/go.mod replace no module,
# and no go.work changes them. Usage: check-tool-origin.sh [repo root]
set -eu
root=${1:-$(dirname "$0")/..}
node - "$root/web/package-lock.json" <<'JS'
const fs = require('fs');
const lock = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
const pkgs = lock.packages ?? {};
let bad = 0;
for (const name of ['eslint', 'typescript', 'openapi-typescript', 'prettier', 'vitest', 'vite', '@playwright/test']) {
  if (!pkgs['node_modules/' + name]) { console.error(`${name}: missing in package-lock.json`); bad++; }
}
// Every installed package, the tools and all they load: from the registry,
// with a checksum, never a link, a file or git.
for (const [path, p] of Object.entries(pkgs)) {
  if (path === '' || p.inBundle) continue; // bundled: inside the checked tarball of its parent
  if (p.link || !String(p.resolved || '').startsWith('https://registry.npmjs.org/') || !p.integrity) {
    console.error(`${path}: not from registry.npmjs.org with a checksum (${p.resolved || 'no resolved'})`); bad++;
  }
}
process.exit(bad ? 1 : 0);
JS
for m in "$root/go.mod" "$root/tools/go.mod"; do
  if grep -Eq '^[[:space:]]*replace([[:space:]]|\()' "$m"; then
    echo "${m#"$root"/} replaces a module" >&2
    exit 1
  fi
done
for w in go.work tools/go.work; do
  if [ -e "$root/$w" ]; then
    echo "$w would change the modules the checks build with" >&2
    exit 1
  fi
done
echo "tool origin ok"
