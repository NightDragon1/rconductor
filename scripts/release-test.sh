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
expect "v1.1 tags" "1.1 1" "$(field v1.1 tags)"
expect "v1.1 rc" false "$(field v1.1 rc)"
expect "v1.1-rc1 tags" 1.1-rc1 "$(field v1.1-rc1 tags)"
expect "v1.1-rc1 prerelease" true "$(field v1.1-rc1 prerelease)"
expect "v1.1-rc1 rc" true "$(field v1.1-rc1 rc)"
expect "v1.10-rc12 tags" 1.10-rc12 "$(field v1.10-rc12 tags)"
expect "v1.10-rc12 version" 1.10-rc12 "$(field v1.10-rc12 version)"

for tag in v1.0.0 1.0 v1 latest v0 v1.1-rc v1.1rc1 v1.1-rc1a v1.1-rc0 v1.1-RC1 v1.1-rc01 1.1-rc1; do
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
expect "notes of a release candidate" "Release candidate, not for production" "$(scripts/release-notes.sh 1.1-rc1 true | head -n 1)"
expect "a release candidate names its image tag" 2 "$(scripts/release-notes.sh 1.1-rc1 true | grep -c ':1.1-rc1`')"
if scripts/release-notes.sh 1.1-rc1 true | grep -q -e "Testversion vor 1.0" -e "^## 1"; then
  echo "a release candidate must carry neither test version notes nor a changelog section" >&2
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
for kind in panel agent; do
  expect "overview $kind of a test version has a quick start" 1 "$(hub 0.10 $kind | grep -c '^## Quick start')"
done
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
# Each image has its own text with a quick start, the security note and the
# links; the panel takes the important variables of README.md, the agent
# names its own and the wiki page for a separate host (PRJ-004).
for kind in panel agent; do
  t=$(hub 1.1 $kind)
  for part in "## Quick start" "in your LAN or behind a VPN" "## Links" "https://github.com/NightDragon1/rconductor/wiki" "## Important variables" 'such as `1.1`'; do
    printf '%s\n' "$t" | grep -qF "$part" || { echo "overview $kind 1.1 lacks: $part" >&2; fails=$((fails + 1)); }
  done
  expect "overview $kind has no placeholder left" 0 "$(printf '%s\n' "$t" | grep -c '{version}')"
  expect "overview $kind keeps the links" 4 "$(printf '%s\n' "$t" | sed -n '/^## Links/,$p' | grep -c '^- ')"
done
vars() { awk '/^## / { on = ($0 == "## Important variables") } on && /^\|/'; }
expect "panel overview has the variables of the README" "$(vars < README.md)" "$(hub 1.1 panel | vars)"
expect "agent overview points to the wiki page for a separate host" 1 "$(hub 1.1 agent | grep -c 'wiki/Agent-on-another-host')"
if [ "$(hub 1.1 panel)" = "$(hub 1.1 agent)" ]; then
  echo "panel and agent overviews must differ" >&2
  fails=$((fails + 1))
fi
# Every variable the agent text names is in the configuration guide.
for v in $(hub 1.1 agent | sed -n 's/^| `\(RCD_[A-Z_]*\|DOCKER_GID\)`.*/\1/p'); do
  grep -q "\`$v\`" docs/user/configuration.md || { echo "agent overview names $v, which docs/user/configuration.md lacks" >&2; fails=$((fails + 1)); }
done
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
# A line "tag" holds the image sha256:i, a line "tag=digest" another one.
d=$(awk -v t="${ref##*:}" '$0 == t { print "sha256:i"; exit } index($0, t "=") == 1 { print substr($0, length(t) + 2); exit }' "$WORLD/tags")
if [ -n "$d" ]; then echo "$d"; exit 0; fi
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
verify v1.1-rc1 1.1-rc1 "" || { echo "verify: a good release candidate failed" >&2; fails=$((fails + 1)); }
# The usual case: an earlier release holds the major tag, which stays.
verify v1.1-rc2 "1.1-rc2 1=sha256:old 1.0=sha256:old" "" ||
  { echo "verify: a release candidate next to an earlier release failed" >&2; fails=$((fails + 1)); }
for bad in "v1.1-rc1|1.1-rc1 1.1|" "v1.1-rc1|1.1-rc1 1|" "v1.1-rc2|1.1-rc2 1=sha256:old 1.1=sha256:old|" "v1.1-rc1|1.1-rc1 latest|" "v1.1-rc1||" "v0.10|0.10 latest|" "v0.10|0.10 0|" "v0.10|0.10 1|" "v1.0|1.0|" "v0.10|0.10|bad-arm64" "v0.10|0.10|wrong-sbom"; do
  tag=${bad%%|*} rest=${bad#*|}
  if verify "$tag" "${rest%%|*}" "${rest#*|}"; then
    echo "verify: must fail: $bad" >&2
    fails=$((fails + 1))
  fi
done

# The Docker Hub step is skipped for a release candidate.
expect "describe skips a release candidate" 1 "$(grep -c "if: github.event_name == 'push' && needs.version.outputs.rc != 'true'" .github/workflows/release.yml)"

# C4 (regression test): scripts/dockerhub-describe.sh, which the release run
# and the description workflow both call, stops before Docker Hub hears of
# anything (no login either) when an overview text fails or is empty. It runs
# against stand-ins for curl and the overview script.
for wf in release.yml dockerhub-describe.yml; do
  expect "C4: $wf calls the describe script" 1 "$(grep -c 'run: scripts/dockerhub-describe.sh "$VERSION"' .github/workflows/$wf)"
done
hubstep=$(mktemp -d)
mkdir -p "$hubstep/bin" "$hubstep/scripts"
cp scripts/dockerhub-describe.sh "$hubstep/scripts/"
cat > "$hubstep/bin/curl" <<'STUB'
#!/bin/sh
echo curl >> "$CALLS"
out=/dev/null
while [ $# -gt 0 ]; do
  case "$1" in -o) out=$2; shift ;; -X) echo "$2" >> "$CALLS" ;; esac
  shift
done
cat > /dev/null
echo '{"access_token":"t"}' > "$out"
printf 200
STUB
chmod +x "$hubstep/bin/curl"
describe() { # how the stand-in overview script behaves: fail, empty, agent-empty or ok
  case $1 in
    fail) printf '#!/bin/sh\necho broken >&2\nexit 1\n' ;;
    empty) printf '#!/bin/sh\nexit 0\n' ;;
    agent-empty) printf '#!/bin/sh\nfor a; do :; done\n[ "$a" = agent ] || echo text\n' ;;
    ok) printf '#!/bin/sh\necho text\n' ;;
  esac > "$hubstep/scripts/dockerhub-overview.sh"
  chmod +x "$hubstep/scripts/dockerhub-overview.sh"
  : > "$hubstep/calls"
  (CALLS=$hubstep/calls PATH="$hubstep/bin:$PATH" HUB=docker.io/x \
    DOCKERHUB_USERNAME=u DOCKERHUB_TOKEN=t "$hubstep/scripts/dockerhub-describe.sh" 1.1 >/dev/null 2>&1)
}
for mode in fail empty agent-empty; do
  if describe "$mode"; then
    echo "C4: the describe script went on with an overview that is $mode" >&2
    fails=$((fails + 1))
  fi
  expect "C4: no call to Docker Hub with an overview that is $mode" 0 "$(grep -c . "$hubstep/calls")"
done
# With working texts the script logs in once and sets both images.
describe ok || { echo "C4: the describe script failed with working texts" >&2; fails=$((fails + 1)); }
expect "C4: descriptions sent with working texts" 2 "$(grep -cx PATCH "$hubstep/calls")"
expect "C4: one login" 3 "$(grep -cx curl "$hubstep/calls")"
rm -rf "$hubstep"

[ "$fails" -eq 0 ] || exit 1
echo "release scripts ok"
