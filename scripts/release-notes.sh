#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Prints the notes of a release (PRJ-003): the section of the version from
# CHANGELOG.md, or a fixed text for a release candidate, which has no entry.
# Then where the signed images are.
set -eu
[ $# -eq 2 ] || { echo "usage: release-notes.sh VERSION PRERELEASE" >&2; exit 2; }
version=$1
if [ "$2" = true ]; then
  echo "Release candidate of $version for testing. Not for production use."
else
  notes=$(awk -v h="## $version" '$0 == h { on = 1; next } /^## / { on = 0 } on' CHANGELOG.md)
  [ -n "$(printf '%s' "$notes" | tr -d '[:space:]')" ] || {
    echo "no entry for $version in CHANGELOG.md" >&2
    exit 1
  }
  printf '%s\n' "$notes"
fi
cat <<EOF

## Images

For amd64 and arm64, signed with cosign and with an SBOM attestation:

- \`ghcr.io/nightdragon1/rconductor-panel:$version\`, \`ghcr.io/nightdragon1/rconductor-agent:$version\`
- \`docker.io/nightdragon1/rconductor-panel:$version\`, \`docker.io/nightdragon1/rconductor-agent:$version\`

The SBOMs are attached to this release.
EOF
