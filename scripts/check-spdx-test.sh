#!/bin/sh
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Tests of check-spdx.sh (NFR-021) on small repositories.
set -eu
# A hook sets these for the repository it runs in; the test repositories
# below must not reach it.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_OBJECT_DIRECTORY GIT_COMMON_DIR GIT_PREFIX
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fail=0
repo() { # repo <dir>: a repository with one licensed file and the root REUSE.toml
  mkdir -p "$1/scripts" "$1/internal"
  cp "$here/check-spdx.sh" "$1/scripts/"
  printf '// SPDX-FileCopyrightText: 2026 NightDragon\n// SPDX-License-Identifier: AGPL-3.0-or-later\npackage a\n' >"$1/internal/a.go"
  printf '# SPDX-FileCopyrightText: 2026 NightDragon\n# SPDX-License-Identifier: AGPL-3.0-or-later\nversion = 1\n' >"$1/REUSE.toml"
}
check() { # check <label> <want exit> <dir>
  git -C "$3" init -q
  git -C "$3" add -A
  if sh "$3/scripts/check-spdx.sh" >/dev/null 2>&1; then got=0; else got=1; fi
  if [ "$got" != "$2" ]; then echo "FAIL $1: exit $got, want $2"; fail=1; fi
}
repo "$tmp/ok"
check "headers and the root REUSE.toml" 0 "$tmp/ok"
repo "$tmp/nested"
printf '# SPDX-FileCopyrightText: 2026 NightDragon\n# SPDX-License-Identifier: AGPL-3.0-or-later\nversion = 1\n' >"$tmp/nested/internal/REUSE.toml"
check "a nested REUSE.toml" 1 "$tmp/nested"
repo "$tmp/dotlicense"
printf 'SPDX-FileCopyrightText: 2026 NightDragon\nSPDX-License-Identifier: AGPL-3.0-or-later\n' >"$tmp/dotlicense/internal/a.go.license"
check "a .license file" 1 "$tmp/dotlicense"
repo "$tmp/umlaut"
mkdir -p "$tmp/umlaut/internal/ä"
printf '# SPDX-FileCopyrightText: 2026 NightDragon\n# SPDX-License-Identifier: AGPL-3.0-or-later\nversion = 1\n' >"$tmp/umlaut/internal/ä/REUSE.toml"
check "a nested REUSE.toml under a quoted path" 1 "$tmp/umlaut"
[ "$fail" = 0 ] && echo "check-spdx-test passed"
exit "$fail"
