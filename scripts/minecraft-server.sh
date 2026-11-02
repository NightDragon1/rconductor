#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Downloads the pinned Minecraft server JAR for the walking skeleton test
# (build tag minecraft) into the given directory, unless it is there
# already, and checks its SHA-1 from Mojang's version manifest. A failed
# download is tried once more. Prints the path of the JAR.
set -eu
VERSION=26.3
SHA1=33680f5f2ac32864d6d7cf5e56a705fdb3e05f4c
URL=https://piston-data.mojang.com/v1/objects/$SHA1/server.jar

dir=${1:?usage: minecraft-server.sh DIR}
mkdir -p "$dir"
dir=$(cd "$dir" && pwd)
jar=$dir/minecraft-server-$VERSION.jar
ok() { [ -f "$jar" ] && echo "$SHA1  $jar" | sha1sum -c --status; }
for attempt in 1 2; do
  ok && break
  rm -f "$jar"
  curl -fsSL --max-time 300 -o "$jar.part" "$URL" && mv "$jar.part" "$jar" ||
    echo "download attempt $attempt failed" >&2
done
if ! ok; then
  echo "no valid server JAR for Minecraft $VERSION" >&2
  exit 1
fi
echo "$jar"
