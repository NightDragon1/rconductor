#!/bin/sh
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Origin of the tools the core checks run (PRJ-005): their packages in
# web/package-lock.json come from registry.npmjs.org, not from a file, a
# link or git; tools/go.mod replaces no module. Usage: check-tool-origin.sh [repo root]
set -eu
root=${1:-$(dirname "$0")/..}
node - "$root/web/package-lock.json" <<'JS'
const fs = require('fs');
const lock = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
const tools = ['eslint', 'typescript', 'openapi-typescript', 'prettier', 'vitest', 'vite', '@playwright/test'];
let bad = 0;
for (const name of tools) {
  const p = lock.packages?.['node_modules/' + name];
  if (!p) { console.error(`${name}: missing in package-lock.json`); bad++; continue; }
  if (p.link || !String(p.resolved || '').startsWith('https://registry.npmjs.org/') || !p.integrity) {
    console.error(`${name}: not from registry.npmjs.org (${p.resolved || 'no resolved'})`); bad++;
  }
}
process.exit(bad ? 1 : 0);
JS
if grep -Eq '^[[:space:]]*replace([[:space:]]|\()' "$root/tools/go.mod"; then
  echo "tools/go.mod replaces a module" >&2
  exit 1
fi
echo "tool origin ok"
