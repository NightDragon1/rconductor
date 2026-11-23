#!/bin/sh
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Scans each platform of an image on its own and signs and attests it at its
# platform digest in both registries, and the index with both SBOMs (T13,
# SEC-013). Needs GHCR, HUB, cosign and syft (or SYFT).
# A cosign call that fails with a network error towards the signing service
# (connection reset, timeout, 5xx) runs again: up to SIGN_ATTEMPTS times
# (default 4, at least 1), SIGN_DELAY seconds apart (default 20, doubled
# each time). Any other error fails at once.
set -eu
[ $# -eq 2 ] || { echo "usage: release-attest.sh TARGET INDEX_DIGEST" >&2; exit 2; }
target=$1
index=$2
image=rconductor-$target
attempts=${SIGN_ATTEMPTS:-4}
delay=${SIGN_DELAY:-20}
case "$attempts$delay" in *[!0-9]*) echo "SIGN_ATTEMPTS and SIGN_DELAY must be numbers" >&2; exit 2 ;; esac
[ "$attempts" -ge 1 ] || { echo "SIGN_ATTEMPTS must be at least 1" >&2; exit 2; }

# sign_retry runs cosign with its arguments and repeats it on a network error.
sign_retry() {
  n=1 wait=$delay
  while :; do
    if cosign "$@" 2>cosign.err; then
      cat cosign.err >&2
      rm -f cosign.err
      return 0
    fi
    cat cosign.err >&2
    if [ "$n" -ge "$attempts" ] ||
      ! grep -qiE 'connection reset|connection refused|timeout|timed out|i/o timeout|TLS handshake|unexpected EOF|no such host|temporary failure|50[0234] ' cosign.err; then
      rm -f cosign.err
      return 1
    fi
    echo "cosign: network error, attempt $n of $attempts; again in $wait s" >&2
    sleep "$wait"
    n=$((n + 1)) wait=$((wait * 2))
  done
}

scripts/release-digests.sh "$GHCR/$image@$index" > platforms
[ "$(cut -d' ' -f1 platforms | sort | tr '\n' ' ')" = "amd64 arm64 " ] || {
  echo "$image: want amd64 and arm64, got: $(cat platforms)" >&2
  exit 1
}
for registry in "$GHCR" "$HUB"; do
  sign_retry sign --yes "$registry/$image@$index"
done
while read -r arch digest; do
  sbom=$image-$arch.spdx.json
  "${SYFT:-syft}" scan --platform "linux/$arch" -o "spdx-json=$sbom" "registry:$GHCR/$image@$digest"
  for registry in "$GHCR" "$HUB"; do
    sign_retry sign --yes "$registry/$image@$digest"
    sign_retry attest --yes --type spdxjson --predicate "$sbom" "$registry/$image@$digest"
    sign_retry attest --yes --type spdxjson --predicate "$sbom" "$registry/$image@$index"
  done
done < platforms
rm platforms
