#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Checks scripts/lockfile-packages.py (PRJ-005, SEC-013) in a throwaway
# repository: new npm packages and Go modules, changed resolved, integrity
# and go.sum hashes (also of /go.mod lines), a package moved or copied to
# another key with a foreign origin, a version bump to a version the base
# had, aliases, new lockfiles, no change, a lockfile missing on the base
# side, and lockfiles or refs it cannot read.
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
    key, name = a[i], None
    if "=" in key:
        key, name = key.split("=")
    pkgs[key] = {"version": a[i + 1], "resolved": a[i + 2], "integrity": a[i + 3]}
    if name:
        pkgs[key]["name"] = name
print(json.dumps({"lockfileVersion": 3, "packages": pkgs}, indent=2))
EOF
}
R=https://registry.npmjs.org
npm node_modules/a 1.0.0 "$R/a/-/a-1.0.0.tgz" sha512-a1 \
  node_modules/a/node_modules/b 1.0.0 "$R/b/-/b-1.0.0.tgz" sha512-b1 \
  node_modules/b 2.0.0 "$R/b/-/b-2.0.0.tgz" sha512-b2
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
# fails_run NAME [REF]: the run fails.
fails_run() {
  if "$repo/scripts/lockfile-packages.py" "${2:-$base}" >/dev/null 2>&1; then
    echo "$1: passed, want a failure" >&2
    fails=$((fails + 1))
  fi
}
# The npm entries of the base.
BASE_NPM="node_modules/a 1.0.0 $R/a/-/a-1.0.0.tgz sha512-a1 node_modules/a/node_modules/b 1.0.0 $R/b/-/b-1.0.0.tgz sha512-b1 node_modules/b 2.0.0 $R/b/-/b-2.0.0.tgz sha512-b2"
# has NAME TEXT: the output contains the line TEXT.
has() {
  grep -qxF -- "$2" <<<"$got" || { echo "$1: missing [$2] in:" >&2; echo "$got" >&2; fails=$((fails + 1)); }
}
hasnt() {
  if grep -qF -- "$2" <<<"$got"; then echo "$1: unexpected [$2] in:" >&2; echo "$got" >&2; fails=$((fails + 1)); fi
}

change "new npm package" <<EOF
npm $BASE_NPM node_modules/a/node_modules/@s/b 2.0.0 "$R/@s/b/-/b-2.0.0.tgz" sha512-b2
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

change "changed resolved" <<EOF
npm node_modules/a 1.0.0 "https://evil.example/a-1.0.0.tgz" sha512-a1 \
  node_modules/a/node_modules/b 1.0.0 "$R/b/-/b-1.0.0.tgz" sha512-b1 node_modules/b 2.0.0 "$R/b/-/b-2.0.0.tgz" sha512-b2
EOF
got=$(out)
has "changed resolved" "- a@1.0.0 (node_modules/a) resolved: $R/a/-/a-1.0.0.tgz -> https://evil.example/a-1.0.0.tgz"
hasnt "changed resolved: not new" "- a@1.0.0 (http"

change "changed integrity" <<EOF
npm node_modules/a 1.0.0 "$R/a/-/a-1.0.0.tgz" sha512-other \
  node_modules/a/node_modules/b 1.0.0 "$R/b/-/b-1.0.0.tgz" sha512-b1 node_modules/b 2.0.0 "$R/b/-/b-2.0.0.tgz" sha512-b2
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

change "moved to another key with a foreign origin" <<EOF
npm node_modules/a 1.0.0 "$R/a/-/a-1.0.0.tgz" sha512-a1 \
  node_modules/b 1.0.0 "https://evil.example/b-1.0.0.tgz" sha512-evil
EOF
got=$(out)
has "moved with a foreign origin" "- b@1.0.0 (node_modules/b) origin not in the base: resolved https://evil.example/b-1.0.0.tgz, integrity sha512-evil"

change "extra copy with a foreign origin" <<EOF
npm $BASE_NPM node_modules/c/node_modules/b 1.0.0 "https://evil.example/b-1.0.0.tgz" sha512-b1
EOF
got=$(out)
has "extra copy with a foreign origin" "- b@1.0.0 (node_modules/c/node_modules/b) origin not in the base: resolved https://evil.example/b-1.0.0.tgz, integrity sha512-b1"

change "bump to a version the base had" <<EOF
npm node_modules/a 1.0.0 "$R/a/-/a-1.0.0.tgz" sha512-a1 \
  node_modules/a/node_modules/b 2.0.0 "$R/b/-/b-2.0.0.tgz" sha512-b2 node_modules/b 2.0.0 "$R/b/-/b-2.0.0.tgz" sha512-b2
EOF
got=$(out)
hasnt "bump to a version the base had, same origin" "b@2.0.0"
change "bump to a version the base had, other origin" <<EOF
npm node_modules/a 1.0.0 "$R/a/-/a-1.0.0.tgz" sha512-a1 \
  node_modules/a/node_modules/b 2.0.0 "https://evil.example/b-2.0.0.tgz" sha512-b2 node_modules/b 2.0.0 "$R/b/-/b-2.0.0.tgz" sha512-b2
EOF
got=$(out)
has "bump to a version the base had, other origin" "- b@2.0.0 (node_modules/a/node_modules/b) origin not in the base: resolved https://evil.example/b-2.0.0.tgz, integrity sha512-b2"

change "alias" <<EOF
npm $BASE_NPM node_modules/x=real 3.0.0 "$R/real/-/real-3.0.0.tgz" sha512-r3
EOF
got=$(out)
has "alias" "- x (alias of real)@3.0.0 ($R/real/-/real-3.0.0.tgz)"

change "changed /go.mod hash" <<'EOF'
printf '%s\n' "example.com/m v1.0.0 h1:old=" "example.com/m v1.0.0/go.mod h1:other=" >go.sum
EOF
got=$(out)
has "changed /go.mod hash" "- example.com/m@v1.0.0/go.mod hash: h1:mod= -> h1:other="

change "new lockfile" <<'EOF'
printf '%s\n' "example.com/w v1.0.0 h1:w=" >web/go.sum
EOF
got=$(out)
has "new lockfile: header" "## web/go.sum"
has "new lockfile: module" "- example.com/w@v1.0.0 (proxy.golang.org, example.com/w)"

change "not JSON" <<'EOF'
echo '{"lockfileVersion": 3, "packages": ' >web/package-lock.json
EOF
fails_run "not JSON"
change "no packages" <<'EOF'
echo '{"lockfileVersion": 3}' >web/package-lock.json
EOF
fails_run "no packages"
change "lockfileVersion 1" <<'EOF'
echo '{"lockfileVersion": 1, "packages": {}}' >web/package-lock.json
EOF
fails_run "lockfileVersion 1"
fails_run "unknown ref" 0000000000000000000000000000000000000000

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
