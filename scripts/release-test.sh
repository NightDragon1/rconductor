#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Checks the release scripts (D-047, PRJ-003): which tag gives which image
# tags, and that a test version never gets a major or latest tag.
set -eu
cd "$(dirname "$0")/.."
GHCR=ghcr.io/x HUB=docker.io/x
export GHCR HUB
fails=0
expect() { # what, want, got
  [ "$2" = "$3" ] || { echo "$1: want [$2], got [$3]" >&2; fails=$((fails + 1)); }
}
field() { scripts/release-version.sh "$1" | sed -n "s/^$2=//p"; }

expect "v1.0 tags" "1.0 1" "$(field v1.0 tags)"
expect "v1.12 tags" "1.12 1" "$(field v1.12 tags)"
expect "v2.0 tags" "2.0 2" "$(field v2.0 tags)"
expect "v1.0 prerelease" false "$(field v1.0 prerelease)"
expect "v0.9 tags" 0.9 "$(field v0.9 tags)"
expect "v0.9 prerelease" true "$(field v0.9 prerelease)"
expect "v0.10 tags" 0.10 "$(field v0.10 tags)"

for tag in v1.0.0 1.0 v1 v1.0-rc1 latest v0; do
  if scripts/release-version.sh "$tag" >/dev/null 2>&1; then
    echo "$tag: must be refused" >&2
    fails=$((fails + 1))
  fi
done

images=$(scripts/release-images.sh panel "$(field v0.9 tags)" | grep -v EOF)
expect "v0.9 images" "ghcr.io/x/rconductor-panel:0.9 docker.io/x/rconductor-panel:0.9" "$(echo $images)"
images=$(scripts/release-images.sh agent "$(field v1.0 tags)" | grep -v EOF | wc -l)
expect "v1.0 image count" 4 "$(echo $images)"

expect "notes of a test version" "Testversion vor 1.0" "$(scripts/release-notes.sh 0.9 true | head -n 1)"
expect "known issues of a test version" "## Known issues" "$(scripts/release-notes.sh 0.9 true | sed -n 3p)"
if scripts/release-notes.sh 1.0 false | grep -q -e "Testversion vor 1.0" -e "To be settled before 1.0"; then
  echo "a real release must not carry the known issues of a test version" >&2
  fails=$((fails + 1))
fi
expect "notes of 1.0" "First release." "$(scripts/release-notes.sh 1.0 false | sed -n 2p)"
if scripts/release-notes.sh 7.0 false >/dev/null 2>&1; then
  echo "a version without a changelog entry must be refused" >&2
  fails=$((fails + 1))
fi

# Docker Hub texts: the pre-release notice only before 1.0, short
# descriptions within the 100 characters Docker Hub allows.
hub() { scripts/dockerhub-overview.sh "$@"; }
expect "short panel 0.10" "RCONductor panel – web panel for RCON game servers. Pre-release (0.x): not for production use." "$(hub --short 0.10 panel)"
expect "short agent 1.0" "RCONductor agent – runs game containers for the panel." "$(hub --short 1.0 agent)"
for v in 0.10 1.0 12.34; do
  for kind in panel agent; do
    len=$(hub --short "$v" "$kind" | tr -d '\n' | wc -m)
    [ "$len" -le 100 ] || { echo "short $v $kind: $len characters, over 100" >&2; fails=$((fails + 1)); }
  done
done
expect "overview of a test version carries the notice" 1 "$(hub 0.10 agent | grep -c '^> \*\*Status: pre-release')"
expect "overview of a test version has the quick start" 1 "$(hub 0.10 agent | grep -c '^## Quick start$')"
# The quick start is the one of README.md, with absolute links only, and
# the whole text fits into Docker Hub (PRJ-004).
section() { awk '/^## / { on = ($0 == "## Quick start") } on'; }
for v in 0.10 1.0; do
  expect "overview of $v has the quick start of the README" "$(section < README.md)" "$(hub "$v" panel | section)"
  expect "overview of $v has no marker left" 0 "$(hub "$v" agent | grep -c '<!--')"
  expect "overview of $v has only absolute links" 0 "$(hub "$v" panel | grep -o '](\([^)]*\))' | grep -vc '^](https://')"
  len=$(hub "$v" panel | wc -m)
  [ "$len" -le 25000 ] || { echo "overview $v: $len characters, over 25000" >&2; fails=$((fails + 1)); }
done
expect "a test version has no line of stable tags" 0 "$(hub 0.10 agent | grep -c '^Tags: ')"
expect "overview of 1.0 names the tags" 1 "$(hub 1.0 panel | grep -c '^Tags: `1` (newest 1.x)')"
if hub 1.0 panel | grep -qi -e pre-release -e '^>'; then
  echo "the overview of 1.0 must not carry the pre-release notice" >&2
  fails=$((fails + 1))
fi
expect "overview of 1.0 keeps the links" 4 "$(hub 1.0 panel | grep -c '^- ')"
expect "overview of 1.0 has no double blank line" 0 "$(hub 1.0 panel | awk 'p == "" && $0 == "" { n++ } { p = $0 } END { print n + 0 }')"
for bad in "1.0 web" "1.0.0 panel" "v1.0 panel" "latest agent"; do
  # shellcheck disable=SC2086 # split on purpose
  if hub $bad >/dev/null 2>&1; then
    echo "dockerhub-overview $bad: must be refused" >&2
    fails=$((fails + 1))
  fi
done

# release-verify.sh against stand-ins for docker and cosign. The stand-ins
# read what the registry holds from $WORLD.
world=$(mktemp -d)
trap 'rm -rf "$world"' EXIT
cat > "$world/docker" <<'STUB'
#!/bin/sh
for ref; do :; done
if [ "$4" = --raw ]; then
  printf '{"manifests":[{"platform":{"os":"linux","architecture":"amd64"},"digest":"sha256:a"},{"platform":{"os":"linux","architecture":"arm64"},"digest":"sha256:b"},{"platform":{"os":"unknown","architecture":"unknown"},"digest":"sha256:c"}]}'
  exit 0
fi
if grep -qx "${ref##*:}" "$WORLD/tags"; then echo sha256:i; exit 0; fi
echo "$ref: not found" >&2
exit 1
STUB
cat > "$world/cosign" <<'STUB'
#!/bin/sh
case "$2" in *@sha256:b) grep -qx bad-arm64 "$WORLD/faults" && exit 1 ;; esac
[ "$1" = verify ] && exit 0
arch=amd64
case "$2" in *@sha256:b) arch=arm64 ;; esac
grep -qx wrong-sbom "$WORLD/faults" && arch=amd64
printf '{"payload":"%s"}\n' "$(printf 'pkg:deb/x?arch=%s pkg:deb/y?arch=all' "$arch" | base64 -w0)"
STUB
chmod +x "$world/docker" "$world/cosign"
verify() { # tag, tags in the registry, faults
  printf '%s\n' $2 > "$world/tags"
  printf '%s\n' "$3" > "$world/faults"
  WORLD=$world PATH="$world:$PATH" scripts/release-verify.sh "$1" >/dev/null 2>&1
}
verify v0.10 0.10 "" || { echo "verify: a good test version failed" >&2; fails=$((fails + 1)); }
verify v1.0 "1.0 1" "" || { echo "verify: a good release failed" >&2; fails=$((fails + 1)); }
for bad in "v0.10|0.10 latest|" "v0.10|0.10 0|" "v0.10|0.10 1|" "v1.0|1.0|" "v0.10|0.10|bad-arm64" "v0.10|0.10|wrong-sbom"; do
  tag=${bad%%|*} rest=${bad#*|}
  if verify "$tag" "${rest%%|*}" "${rest#*|}"; then
    echo "verify: must fail: $bad" >&2
    fails=$((fails + 1))
  fi
done

[ "$fails" -eq 0 ] || exit 1
echo "release scripts ok"
