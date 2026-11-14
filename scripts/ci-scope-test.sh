#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Checks scripts/ci-scope.sh (NFR-013) against a throwaway repository: each
# changed path counts with its real name, a moved file with both paths, and
# a path the script does not know turns every stage on. CI_SCOPE names
# another version of the script to check.
set -eu
cd "$(dirname "$0")/.."
scope=$(realpath "${CI_SCOPE:-scripts/ci-scope.sh}")
fails=0
expect() { # what, want, got
  [ "$2" = "$3" ] || { echo "$1: want [$2], got [$3]" >&2; fails=$((fails + 1)); }
}

repo=$(mktemp -d)
trap 'rm -rf "$repo"' EXIT
# No global or system configuration: the defaults of git count, with
# rename detection on as in a plain git diff.
GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_CONFIG_GLOBAL GIT_CONFIG_NOSYSTEM
# Run from a git hook, these point at the calling repository; the
# throwaway repository must not touch it.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX
git -C "$repo" init -q
git -C "$repo" config user.name test
git -C "$repo" config user.email test@example.invalid
git -C "$repo" config diff.renames true
mkdir -p "$repo/web/src" "$repo/internal/rcon" "$repo/docs"
echo "export const a = 1;" >"$repo/web/src/a.ts"
echo "package rcon" >"$repo/internal/rcon/rcon.go"
echo "docs" >"$repo/docs/index.md"
git -C "$repo" add -A
git -C "$repo" commit -qm base
base=$(git -C "$repo" rev-parse HEAD)

# change NAME: a commit on top of the base, made by the commands that
# follow on standard input in the repository.
change() {
  git -C "$repo" checkout -q --detach "$base"
  (cd "$repo" && sh -eu)
  git -C "$repo" add -A
  git -C "$repo" commit -qm "$1"
}
field() { # name of the output
  (cd "$repo" && EVENT=pull_request REF=refs/pull/1/merge BASE=$base sh "$scope") | sed -n "s/^$1=//p"
}

change "docs only" <<'EOF'
echo more >>docs/index.md
EOF
expect "a docs change: e2e" false "$(field e2e)"
expect "a docs change: minecraft" false "$(field minecraft)"
expect "a docs change: sqlc" false "$(field sqlc)"
expect "a docs change: code" false "$(field code)"

change "docs and assets only" <<'EOF'
mkdir -p assets/other .github/ISSUE_TEMPLATE
echo svg >assets/other/a.svg
echo t >.github/ISSUE_TEMPLATE/bug.md
echo l >LICENSE
echo c >>CHANGELOG.md
EOF
expect "docs, assets, licence, issue template: code" false "$(field code)"
expect "docs, assets, licence, issue template: e2e" false "$(field e2e)"

for f in README.md docs/user/configuration.md docs/user/technical.md assets/logo/rconductor-mark.svg; do
  change "tested doc $f" <<EOF
mkdir -p docs/user assets/logo
echo x >>$f
EOF
  expect "a doc that tests read ($f): code" true "$(field code)"
done

change "go.mod" <<'EOF'
echo "module x" >go.mod
EOF
expect "go.mod: code" true "$(field code)"

change "code with docs" <<'EOF'
echo "export const d = 4;" >>web/src/a.ts
echo x >web/README.md
EOF
expect "code with docs: code" true "$(field code)"

change "ascii web" <<'EOF'
echo "export const b = 2;" >>web/src/a.ts
EOF
expect "an ASCII path under web/: e2e" true "$(field e2e)"
expect "an ASCII path under web/: minecraft" false "$(field minecraft)"

change "non-ascii web" <<'EOF'
echo "export const c = 3;" >"web/src/$(printf '\303\244').ts"
EOF
expect "C26: a non-ASCII path under web/: e2e" true "$(field e2e)"

change "newline rcon" <<'EOF'
echo "package rcon" >"internal/rcon/$(printf 'a\nb').go"
EOF
expect "C26: a newline in a name under internal/rcon/: minecraft" true "$(field minecraft)"
expect "C26: a newline in a name under internal/rcon/: e2e" true "$(field e2e)"

change "move out of web" <<'EOF'
git mv web/src/a.ts docs/a.ts
EOF
expect "C26: a file moved out of web/: e2e" true "$(field e2e)"

change "unknown top level" <<'EOF'
mkdir newtop
echo new >newtop/file.txt
EOF
expect "C26: an unknown path: e2e" true "$(field e2e)"
expect "C26: an unknown path: minecraft" true "$(field minecraft)"
expect "C26: an unknown path: sqlc" true "$(field sqlc)"
expect "an unknown path: code" true "$(field code)"

[ "$fails" -eq 0 ] || exit 1
echo "ci scope ok"
