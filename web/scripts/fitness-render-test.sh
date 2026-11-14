#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Negative test of the render fitness function (PRJ-005): the test project
# breaks the rule once for the script (exit 1, not a crash) and once for ESLint;
# a second one leaves a file out of its tsconfig (still reported), a third
# has a .jsx file, a fourth imports a file from outside src/ (both exit 3).
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
# A source file or declaration imported from outside src/ fails it (exit 3).
o=scripts/testdata/render-outside
out=$(node scripts/fitness-render.ts "$o" "$o/openapi.yaml" 2>&1)
rc=$?
[ "$rc" -eq 3 ] && printf '%s' "$out" | grep -q 'outside src/: .*lib/Away.tsx' ||
  { echo "render script: exit $rc on a file outside src/, want 3" >&2; exit 1; }
t=scripts/testdata/render-outside-dts
out=$(node scripts/fitness-render.ts "$t" "$t/openapi.yaml" 2>&1)
rc=$?
[ "$rc" -eq 3 ] && printf '%s' "$out" | grep -q 'outside src/: .*lib/away.d.ts' ||
  { echo "render script: exit $rc on types outside src/, want 3" >&2; exit 1; }
# ESLint runs with the fixed core config (-c, no lookup) and must report
# the rule (exit 1; exit 2 is a broken config): in the test project, and
# below a nested eslint.config.js that turns the rule off.
eslint() { node node_modules/eslint/bin/eslint.js -c eslint.config.js --no-ignore "$1" 2>&1; }
for p in "$d/src" scripts/testdata/render-eslint/src; do
  out=$(eslint "$p")
  rc=$?
  [ "$rc" -eq 1 ] && printf '%s' "$out" | grep -q 'Use ForeignText' &&
    printf '%s' "$out" | grep -q "$p/App.tsx" ||
    { echo "eslint: exit $rc in $p, want 1 with the HTML rule on App.tsx" >&2; exit 1; }
done
echo "render fitness test ok"
