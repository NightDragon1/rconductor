#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Scans each platform of an image on its own and signs and attests it at its
# platform digest in both registries, and the index with both SBOMs (T13,
# SEC-013). Needs GHCR, HUB, cosign and syft (or SYFT).
set -eu
[ $# -eq 2 ] || { echo "usage: release-attest.sh TARGET INDEX_DIGEST" >&2; exit 2; }
target=$1
index=$2
image=rconductor-$target

scripts/release-digests.sh "$GHCR/$image@$index" > platforms
[ "$(cut -d' ' -f1 platforms | sort | tr '\n' ' ')" = "amd64 arm64 " ] || {
  echo "$image: want amd64 and arm64, got: $(cat platforms)" >&2
  exit 1
}
for registry in "$GHCR" "$HUB"; do
  cosign sign --yes "$registry/$image@$index"
done
while read -r arch digest; do
  sbom=$image-$arch.spdx.json
  "${SYFT:-syft}" scan --platform "linux/$arch" -o "spdx-json=$sbom" "registry:$GHCR/$image@$digest"
  for registry in "$GHCR" "$HUB"; do
    cosign sign --yes "$registry/$image@$digest"
    cosign attest --yes --type spdxjson --predicate "$sbom" "$registry/$image@$digest"
    cosign attest --yes --type spdxjson --predicate "$sbom" "$registry/$image@$index"
  done
done < platforms
rm platforms
