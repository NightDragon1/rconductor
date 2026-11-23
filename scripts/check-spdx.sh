#!/bin/sh
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Fails if a tracked text file lacks an SPDX license header in its first
# lines. Exempt: docs/, Markdown, LICENSE, LICENSES/, AUTHORS, JSON (no
# comment syntax), Go module files, generated code, placeholders and binary
# files; REUSE.toml licenses them, and reuse lint checks the whole tree.
# This script keeps the rules reuse lint does not know: AGPL-3.0-or-later
# for own files, MIT only for the copied files below.
# sqlc query files carry the header at the end, because sqlc copies leading
# comments into the generated code.
set -eu
cd "$(dirname "$0")/.."
# Licenses come only from file headers and the REUSE.toml at the root
# (core path): a nested REUSE.toml or a .license file beside a file would
# license it past that file (NFR-021).
others=$(git -c core.quotePath=false ls-files | grep -E '(^|/)REUSE\.toml$|\.license$' | grep -vx 'REUSE.toml' || true)
if [ -n "$others" ]; then
  echo "licenses only from headers and the REUSE.toml at the root, not from:"
  echo "$others"
  exit 1
fi
# REUSE-IgnoreStart
missing=$(git -c core.quotePath=false ls-files |
  grep -vE '^docs/|\.md$|^LICENSE$|^LICENSES/|^AUTHORS$|\.json$|(^|/)go\.(mod|sum)$|(^|/)\.gitkeep$' |
  while IFS= read -r f; do
    grep -Iq . "$f" 2>/dev/null || continue # binary or empty
    head -n 5 "$f" | grep -qiE 'code generated|auto-generated' && continue
    case "$f" in
      internal/store/queries/*.sql | internal/store/queries/*/*.sql) lines=$(tail -n 3 "$f") ;;
      *) lines=$(head -n 5 "$f") ;;
    esac
    # Copied third-party code keeps its MIT license; only the files listed
    # in THIRD_PARTY_NOTICES.md may (PRJ-002). shadcn/ui is a template to
    # copy, not a dependency, so MIT is allowed for these files alone; any
    # other file with MIT fails here.
    case "$f" in
      web/src/components/ui/* | web/src/lib/utils.ts)
        echo "$lines" | grep -q 'SPDX-License-Identifier: MIT' && echo "$lines" | grep -q 'THIRD_PARTY_NOTICES' || echo "$f"
        continue
        ;;
    esac
    echo "$lines" | grep -q 'SPDX-License-Identifier: AGPL-3.0-or-later' || echo "$f"
  done)
# REUSE-IgnoreEnd
if [ -n "$missing" ]; then
  echo "missing SPDX header:"
  echo "$missing"
  exit 1
fi
