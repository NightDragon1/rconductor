#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Checks scripts/lockfile-packages.py (PRJ-005, SEC-013) in a throwaway
# repository: new npm packages and Go modules, changed resolved, integrity
# and go.sum hashes, /go.mod lines ignored, no change and a lockfile missing
# on the base side.
set -eu
src=$(cd "$(dirname "$0")/.." && pwd)
repo=$(mktemp -d)
trap 'rm -rf "$repo"' EXIT
fails=0
GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_CONFIG_GLOBAL GIT_CONFIG_NOSYSTEM
# Run from a git hook, these point at the calling repository; the
# throwaway repository must not touch it.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_COMMON_DIR GIT_PREFIX
git -C "$repo" init -q
git -C "$repo" config user.name test
git -C "$repo" config user.email test@example.invalid
mkdir -p "$repo/scripts" "$repo/web" "$repo/tools"
cp "$src/scripts/lockfile-packages.py" "$repo/scripts/"

# npm KEY VERSION RESOLVED INTEGRITY...: a package-lock.json with these entries.
npm() {
  python3 - "$@" >"$repo/web/package-lock.json" <<'EOF'
import json, sys
a = sys.argv[1:]
pkgs = {"": {"name": "web"}}
for i in range(0, len(a), 4):
    pkgs[a[i]] = {"version": a[i + 1], "resolved": a[i + 2], "integrity": a[i + 3]}
print(json.dumps({"lockfileVersion": 3, "packages": pkgs}, indent=2))
EOF
}
R=https://registry.npmjs.org
npm node_modules/a 1.0.0 "$R/a/-/a-1.0.0.tgz" sha512-a1
printf '%s\n' \
  "example.com/m v1.0.0 h1:old=" \
  "example.com/m v1.0.0/go.mod h1:mod=" >"$repo/go.sum"
git -C "$repo" add -A
git -C "$repo" commit -qm base
base=$(git -C "$repo" rev-parse HEAD)

# change NAME: a commit on top of the base, made by the commands that
# follow on standard input in the repository.
change() {
  git -C "$repo" checkout -q --detach "$base"
  (cd "$repo" && eval "$(cat)")
  git -C "$repo" add -A
  git -C "$repo" commit -qm "$1" --allow-empty
}
out() { "$repo/scripts/lockfile-packages.py" "$base"; }
# has NAME TEXT: the output contains the line TEXT.
has() {
  grep -qxF -- "$2" <<<"$got" || { echo "$1: missing [$2] in:" >&2; echo "$got" >&2; fails=$((fails + 1)); }
}
hasnt() {
  if grep -qF -- "$2" <<<"$got"; then echo "$1: unexpected [$2] in:" >&2; echo "$got" >&2; fails=$((fails + 1)); fi
}

change "new npm package" <<EOF
npm node_modules/a 1.0.0 "$R/a/-/a-1.0.0.tgz" sha512-a1 \
  node_modules/a/node_modules/@s/b 2.0.0 "$R/@s/b/-/b-2.0.0.tgz" sha512-b2
EOF
got=$(out)
has "new npm package" "- @s/b@2.0.0 ($R/@s/b/-/b-2.0.0.tgz)"
hasnt "new npm package: old one" "- a@1.0.0"

change "new go module" <<'EOF'
printf '%s\n' "example.com/n v0.2.0 h1:n=" "example.com/n v0.2.0/go.mod h1:nmod=" >tools/go.sum
EOF
got=$(out)
has "new go module" "- example.com/n@v0.2.0 (proxy.golang.org, example.com/n)"
hasnt "a /go.mod line is ignored" "v0.2.0/go.mod"
has "lockfile missing on the base side: header" "## tools/go.sum"

change "changed resolved" <<EOF
npm node_modules/a 1.0.0 "https://evil.example/a-1.0.0.tgz" sha512-a1
EOF
got=$(out)
has "changed resolved" "- a@1.0.0 (node_modules/a) resolved: $R/a/-/a-1.0.0.tgz -> https://evil.example/a-1.0.0.tgz"
hasnt "changed resolved: not new" "- a@1.0.0 (http"

change "changed integrity" <<EOF
npm node_modules/a 1.0.0 "$R/a/-/a-1.0.0.tgz" sha512-other
EOF
got=$(out)
has "changed integrity" "- a@1.0.0 (node_modules/a) integrity: sha512-a1 -> sha512-other"

change "changed go.sum hash" <<'EOF'
printf '%s\n' "example.com/m v1.0.0 h1:new=" "example.com/m v1.0.0/go.mod h1:mod=" >go.sum
EOF
got=$(out)
has "changed go.sum hash" "- example.com/m@v1.0.0 hash: h1:old= -> h1:new="
hasnt "changed go.sum hash: not new" "(proxy.golang.org, example.com/m)"

change "go.mod line only" <<'EOF'
printf '%s\n' "example.com/m v1.0.0 h1:old=" "example.com/m v1.0.0/go.mod h1:mod=" "example.com/x v3.0.0/go.mod h1:x=" >go.sum
EOF
got=$(out)
hasnt "a /go.mod-only line is ignored" "example.com/x"

change "no change" <<'EOF'
true
EOF
got=$(out)
want="## web/package-lock.json
New packages:
none
Changed origin or checksum:
none

## go.sum
New packages:
none
Changed origin or checksum:
none

## tools/go.sum
New packages:
none
Changed origin or checksum:
none"
if [ "$got" != "$want" ]; then
  echo "no change: want none everywhere, got:" >&2
  echo "$got" >&2
  fails=$((fails + 1))
fi

[ "$fails" -eq 0 ] || exit 1
echo "lockfile packages ok"
