#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Negative test of the render fitness function (PRJ-005): the test project
# breaks the rule once for the script (exit 1, not a crash) and once for ESLint;
# a second one leaves a file out of its tsconfig (exit 2).
d=scripts/testdata/render-bad
node scripts/fitness-render.ts "$d" "$d/openapi.yaml" >/dev/null
rc=$?
[ "$rc" -eq 1 ] || { echo "render script: exit $rc on the test project, want 1" >&2; exit 1; }
# A tsconfig that leaves a source file out must not narrow the check.
n=scripts/testdata/render-narrow
node scripts/fitness-render.ts "$n" "$n/openapi.yaml" >/dev/null 2>&1
rc=$?
[ "$rc" -eq 2 ] || { echo "render script: exit $rc on a narrowed tsconfig, want 2" >&2; exit 1; }
if npx eslint --no-ignore "$d/src" >/dev/null 2>&1; then
  echo "eslint passes dangerouslySetInnerHTML in the test project" >&2
  exit 1
fi
echo "render fitness test ok"
