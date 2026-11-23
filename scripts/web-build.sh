#!/bin/sh
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Builds the UI as CI and the image do (PRJ-005): the tools start with node
# from node_modules, not through the npm scripts of web/package.json, and
# Vite reads only vite.config.ts; another vite.config.* would come first,
# so it fails the build.
set -eu
cd "$(dirname "$0")/../web"
for f in vite.config.*; do
  [ "$f" = vite.config.ts ] || { echo "web/$f is not allowed: Vite would read it before vite.config.ts" >&2; exit 1; }
done
node node_modules/typescript/bin/tsc -b
node node_modules/vite/bin/vite.js build --config vite.config.ts
