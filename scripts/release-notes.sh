#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Prints the notes of a release (PRJ-003): the section of the version from
# CHANGELOG.md, or a fixed text for a test version, which has no entry.
# Then where the signed images are.
set -eu
[ $# -eq 2 ] || { echo "usage: release-notes.sh VERSION PRERELEASE" >&2; exit 2; }
version=$1
if [ "$2" = true ]; then
  cat <<'NOTES'
Testversion vor 1.0

## Known issues

To be fixed before 1.0:

- "Earlier lines" in the logs can double lines (#154)
- Live logs can end without notice after several switches between console and logs (#155, #156)
- Repeating log lines can get lost after coming back to a server (#157, #149)
- Smaller findings of the reviews (#158, #160) and gaps in the console (#150)
NOTES
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
