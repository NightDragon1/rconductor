#!/bin/sh
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
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
# Calls, element access, template literals, names bound to a field, the
# elements of a foreign list, attributes and mapped values count (exit 1,
# nine lines); the three lines marked ok in the project are not reported.
w=scripts/testdata/render-wide
out=$(node scripts/fitness-render.ts "$w" "$w/openapi.yaml" 2>&1)
rc=$?
n=$(printf '%s\n' "$out" | grep -c 'shown without ForeignText')
[ "$rc" -eq 1 ] && [ "$n" -eq 9 ] && ! printf '%s' "$out" | grep -qE 'App.tsx:(34|36|38) ' ||
  { echo "render script: exit $rc with $n findings on the wide project, want 1 with 9" >&2; exit 1; }
# A .jsx file outside src/ enters the program and fails it (exit 3).
x=scripts/testdata/render-outside-jsx
out=$(node scripts/fitness-render.ts "$x" "$x/openapi.yaml" 2>&1)
rc=$?
[ "$rc" -eq 3 ] && printf '%s' "$out" | grep -q 'outside src/: .*lib/Away.jsx' ||
  { echo "render script: exit $rc on a .jsx file outside src/, want 3" >&2; exit 1; }
# A mark the reader does not see fails the check (exit 2).
i=scripts/testdata/render-indent
out=$(node scripts/fitness-render.ts "$i" "$i/openapi.yaml" 2>&1)
rc=$?
[ "$rc" -eq 2 ] && printf '%s' "$out" | grep -q 'x-foreign marks' ||
  { echo "render script: exit $rc on a mark at another indent, want 2" >&2; exit 1; }
# ESLint runs with the fixed core config (-c, no lookup) and must report
# the rule (exit 1; exit 2 is a broken config): in the test project, below
# a nested eslint.config.js that turns the rule off, and behind an
# eslint-disable comment.
eslint() { node node_modules/eslint/bin/eslint.js -c eslint.config.js --no-ignore "$1" 2>&1; }
for p in "$d/src" scripts/testdata/render-eslint/src scripts/testdata/render-inline/src; do
  out=$(eslint "$p")
  rc=$?
  [ "$rc" -eq 1 ] && printf '%s' "$out" | grep -q 'Use ForeignText' &&
    printf '%s' "$out" | grep -q "$p/App.tsx" ||
    { echo "eslint: exit $rc in $p, want 1 with the HTML rule on App.tsx" >&2; exit 1; }
done
echo "render fitness test ok"
