#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Looks for secrets in the lines a commit adds (SEC-012, NFR-013): private
# keys, GitHub and Docker Hub token prefixes, and password, secret or token
# set to a literal value. Only the staged changes count, on every branch.
# Tests, test data, the e2e stack, the load test and UI texts may name
# passwords; there only keys and token prefixes count. A line with the comment "secret-scan: allow" is skipped.
# Without arguments it reads `git diff --cached`; with a file it reads a diff
# from there (for the tests).
set -euo pipefail
LC_ALL=C
export LC_ALL
diff=${1:-}
if [[ -z "$diff" ]]; then
  input=$(git diff --cached -U0 --no-color --no-ext-diff --diff-filter=ACMR)
else
  input=$(cat "$diff")
fi

key='-----BEGIN ([A-Z]+ )?PRIVATE KEY( BLOCK)?-----'
prefix='(gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,}|dckr_pat_[A-Za-z0-9_-]{20,})'
# password, secret or token at the end of a name, then = or :, then a
# literal value of at least 8 characters. In code the value must be a quoted
# string; elsewhere (env, YAML, docs) a bare value counts too. Variables,
# placeholders (<…>, …, ...), masks (***) and well-known fillers do not.
name='[A-Za-z0-9_.-]*(password|passwd|secret|token)'
quoted='["'"'"'`]([^"'"'"'`[:space:]$<{*][^"'"'"'`[:space:]]{7,})["'"'"'`]'
bare='["'"'"']?([^[:space:]"'"'"'$<{(*][^[:space:]"'"'"']{7,})'
found=0
file=
line=0
while IFS= read -r l; do
  case "$l" in
    '+++ b/'*) file=${l#+++ b/}; continue ;;
    '+++ '*) file=; continue ;;
    '@@ '*)
      line=$(printf '%s' "$l" | sed -E 's/^@@ -[0-9,]+ \+([0-9]+).*/\1/')
      continue
      ;;
    '+'*) ;;
    *) continue ;;
  esac
  text=${l#+}
  n=$line
  line=$((line + 1))
  [[ -n "$file" ]] || continue
  [[ "$text" == *"secret-scan: allow"* ]] && continue
  why=
  if [[ "$text" =~ $key ]]; then
    why="private key"
  elif [[ "$text" =~ $prefix ]]; then
    why="token"
  else
    case "$file" in
      *_test.go | */testdata/* | *.test.ts | *.test.tsx | *.spec.ts | web/e2e/* | internal/e2e/* | scripts/*-test.sh | scripts/loadtest/* | web/src/i18n/*) ;;
      *)
        # In code the name stands outside quotes (not a string such as
        # 'new-password') and the value is a quoted string.
        case "$file" in
          *.go | *.ts | *.tsx | *.js | *.mjs | *.sql)
            assign="(^|[^A-Za-z0-9_\"'\`-])${name}[[:space:]]*(:=|=|:)[[:space:]]*${quoted}"
            ;;
          *)
            assign="(^|[^A-Za-z0-9_])${name}[\"'\"']?[[:space:]]*(:=|=|:)[[:space:]]*${bare}"
            ;;
        esac
        shopt -s nocasematch
        if [[ "$text" =~ $assign ]]; then
          kw=${BASH_REMATCH[2]}
          value=${BASH_REMATCH[4]}
          case "$value" in
            *...* | *…* | os.Getenv* | env.* | process.env* | getenv* | example* | changeme* | xxxxxxxx* | placeholder* | your-* | your_*) ;;
            *) why="literal $kw" ;;
          esac
        fi
        shopt -u nocasematch
        ;;
    esac
  fi
  if [[ -n "$why" ]]; then
    echo "$file:$n: possible secret ($why)" >&2
    found=$((found + 1))
  fi
done <<<"$input"

if ((found)); then
  echo "secret check: $found line(s); remove the secret, or mark a false alarm with \"secret-scan: allow\"" >&2
  exit 1
fi
echo "secret check passed"
