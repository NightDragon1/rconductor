#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Checks a published release from the outside, so that T13 holds for every
# release (SEC-013, D-047): in both registries each image has its tags and no
# other moving tag; index and each platform digest are signed and attested,
# and the SBOM of a platform lists only packages of that platform. Reports
# every fault and fails at the end. Needs GHCR, HUB, cosign, docker and jq.
set -eu
[ $# -eq 1 ] || { echo "usage: release-verify.sh TAG" >&2; exit 2; }
out=$(scripts/release-version.sh "$1")
tags=$(printf '%s\n' "$out" | sed -n 's/^tags=//p')
prerelease=$(printf '%s\n' "$out" | sed -n 's/^prerelease=//p')
identity='^https://github\.com/NightDragon1/rconductor/\.github/workflows/release\.yml@refs/tags/'
issuer=https://token.actions.githubusercontent.com
fails=0
fault() { echo "FAULT: $*" >&2; fails=$((fails + 1)); }

digest_of() { docker buildx imagetools inspect --format '{{.Manifest.Digest}}' "$1"; }

# absent fails unless the registry says the tag does not exist.
absent() {
  if answer=$(digest_of "$1" 2>&1); then
    fault "$1 exists"
  elif ! printf '%s' "$answer" | grep -qiE 'not found|manifest unknown'; then
    fault "$1: no answer: $answer"
  fi
}

signed() {
  cosign verify "$1" --certificate-identity-regexp "$identity" \
    --certificate-oidc-issuer "$issuer" >/dev/null 2>&1 || fault "$1: no valid signature"
}

# attested prints the SBOMs of an image, one JSON statement per line.
attested() {
  cosign verify-attestation "$1" --type spdxjson --certificate-identity-regexp "$identity" \
    --certificate-oidc-issuer "$issuer" 2>/dev/null
}

check_platform() { # image arch
  if ! sboms=$(attested "$1"); then
    fault "$1: no valid SBOM attestation"
    return 0
  fi
  packages=$(printf '%s\n' "$sboms" | jq -r '.payload | @base64d' | grep -oE 'arch=[a-z0-9]+' | sort -u)
  printf '%s\n' "$packages" | grep -qx "arch=$2" || fault "$1: SBOM lists no packages of $2"
  other=$(printf '%s\n' "$packages" | grep -vxE "arch=($2|all)" || true)
  [ -z "$other" ] || fault "$1: SBOM of $2 lists $(echo $other)"
}

for registry in "$GHCR" "$HUB"; do
  for target in panel agent; do
    image=$registry/rconductor-$target
    index=
    for tag in $tags; do
      if ! digest=$(digest_of "$image:$tag" 2>/dev/null); then
        fault "$image:$tag missing"
        continue
      fi
      [ -z "$index" ] || [ "$index" = "$digest" ] || fault "$image:$tag is another image than $index"
      index=$digest
    done
    absent "$image:latest"
    if [ "$prerelease" = true ]; then
      absent "$image:0"
      absent "$image:1"
    fi
    [ -n "$index" ] || continue
    signed "$image@$index"
    attested "$image@$index" >/dev/null || fault "$image@$index: no valid SBOM attestation"
    platforms=$(scripts/release-digests.sh "$image@$index")
    [ "$(printf '%s\n' "$platforms" | cut -d' ' -f1 | sort | tr '\n' ' ')" = "amd64 arm64 " ] ||
      fault "$image: want amd64 and arm64, got: $(echo $platforms)"
    while read -r arch digest; do
      signed "$image@$digest"
      check_platform "$image@$digest" "$arch"
    done <<EOF
$platforms
EOF
  done
done
[ "$fails" -eq 0 ] || { echo "$fails fault(s)" >&2; exit 1; }
echo "release $1 verified"
