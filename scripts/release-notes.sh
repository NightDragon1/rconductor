#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Prints the notes of a release (PRJ-003): the section of the version from
# CHANGELOG.md, or a fixed text for a test version or a release candidate,
# which have no entry. Then where the signed images are.
set -eu
[ $# -eq 2 ] || { echo "usage: release-notes.sh VERSION PRERELEASE" >&2; exit 2; }
version=$1
case $version in
  *-rc*)
    echo "Release candidate, not for production"
    ;;
  *)
    if [ "$2" = true ]; then
      cat <<'NOTES'
Testversion vor 1.0

## Known issues

Coming from 0.9:

- The container label is now `rconductor.manage=true`. Containers with only the old label `rcon.manage=true` are no longer managed: change the label before you update.

To be settled before 1.0:

- The player count of modded Minecraft servers shows nothing until the answer of `list` fits the game profile
NOTES
    else
      notes=$(awk -v h="## $version" '$0 == h { on = 1; next } /^## / { on = 0 } on' CHANGELOG.md)
      [ -n "$(printf '%s' "$notes" | tr -d '[:space:]')" ] || {
        echo "no entry for $version in CHANGELOG.md" >&2
        exit 1
      }
      printf '%s\n' "$notes"
    fi
    ;;
esac
cat <<EOF

## Images

For amd64 and arm64, signed with cosign and with an SBOM attestation:

- \`ghcr.io/nightdragon1/rconductor-panel:$version\`, \`ghcr.io/nightdragon1/rconductor-agent:$version\`
- \`docker.io/nightdragon1/rconductor-panel:$version\`, \`docker.io/nightdragon1/rconductor-agent:$version\`

The SBOMs are attached to this release.
EOF
