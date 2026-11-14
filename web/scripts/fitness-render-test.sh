#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Negative test of the render fitness function (PRJ-005): the test project
# breaks the rule once for the script (exit 1, not a crash) and once for ESLint;
# a second one leaves a file out of its tsconfig (still reported), a third
# has a .jsx file (exit 3).
d=scripts/testdata/render-bad
node scripts/fitness-render.ts "$d" "$d/openapi.yaml" >/dev/null
rc=$?
[ "$rc" -eq 1 ] || { echo "render script: exit $rc on the test project, want 1" >&2; exit 1; }
# A tsconfig that leaves a source file out must not narrow the check: the
# file is still checked and reported.
n=scripts/testdata/render-narrow
out=$(node scripts/fitness-render.ts "$n" "$n/openapi.yaml" 2>&1)
rc=$?
[ "$rc" -eq 1 ] && printf '%s' "$out" | grep -q 'src/Hidden.tsx' ||
  { echo "render script: exit $rc on a narrowed tsconfig, want 1 for src/Hidden.tsx" >&2; exit 1; }
# A source file the check cannot read fails it (exit 3, not a crash).
j=scripts/testdata/render-jsx
out=$(node scripts/fitness-render.ts "$j" "$j/openapi.yaml" 2>&1)
rc=$?
[ "$rc" -eq 3 ] && printf '%s' "$out" | grep -q 'cannot read: .*src/Bad.jsx' ||
  { echo "render script: exit $rc on a .jsx file, want 3" >&2; exit 1; }
if npx eslint --no-ignore "$d/src" >/dev/null 2>&1; then
  echo "eslint passes dangerouslySetInnerHTML in the test project" >&2
  exit 1
fi
echo "render fitness test ok"
