#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Fails if a tracked text file lacks an SPDX license header in its first
# lines. Exempt: the concept in docs/, Markdown, LICENSE, JSON (no comment
# syntax), Go module files, generated code, placeholders and binary files.
# sqlc query files carry the header at the end, because sqlc copies leading
# comments into the generated code.
set -eu
cd "$(dirname "$0")/.."
missing=$(git ls-files |
  grep -vE '^docs/|\.md$|^LICENSE$|\.json$|(^|/)go\.(mod|sum)$|(^|/)\.gitkeep$' |
  while IFS= read -r f; do
    grep -Iq . "$f" 2>/dev/null || continue # binary or empty
    head -n 5 "$f" | grep -qiE 'code generated|auto-generated' && continue
    case "$f" in
      internal/store/queries/*.sql | internal/store/queries/*/*.sql) lines=$(tail -n 3 "$f") ;;
      *) lines=$(head -n 5 "$f") ;;
    esac
    # Copied third-party code keeps its MIT license; only the files listed
    # in THIRD_PARTY_NOTICES.md may (PRJ-002).
    case "$f" in
      web/src/components/ui/* | web/src/lib/utils.ts)
        echo "$lines" | grep -q 'SPDX-License-Identifier: MIT' && echo "$lines" | grep -q 'THIRD_PARTY_NOTICES' || echo "$f"
        continue
        ;;
    esac
    echo "$lines" | grep -q 'SPDX-License-Identifier: AGPL-3.0-or-later' || echo "$f"
  done)
if [ -n "$missing" ]; then
  echo "missing SPDX header:"
  echo "$missing"
  exit 1
fi
