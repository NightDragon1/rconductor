#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Checks the deny list in .claude/settings.json (NFR-013, C27-C30). It only
# reads the JSON: required patterns must be present, no pattern may use the
# legacy ":*" suffix, and a model of the wildcard matching must deny the
# listed forms and leave the allowed commands alone. The model: "*" matches
# any characters, a trailing " *" also matches nothing, and a legacy ":*"
# suffix is read as " *". No command is executed.
# Optional argument: path of the settings file to check.
set -euo pipefail
settings=${1:-"$(cd "$(dirname "$0")/.." && pwd)/.claude/settings.json"}
fail=0

has() {
  if jq -e --arg p "$1" '.permissions.deny | index($p)' "$settings" >/dev/null; then
    echo "ok   $2 has $1"
  else
    echo "FAIL $2 lacks $1"
    fail=1
  fi
}

# matches <command>: prints the deny patterns that match the command.
matches() {
  jq -r --arg c "$1" '
    def esc: gsub("(?<m>[.^$|()\\[\\]{}+?\\\\])"; "\\\(.m)");
    .permissions.deny[]
    | select(startswith("Bash(") and endswith(")"))
    | . as $rule
    | .[5:-1]
    | (if endswith(":*") then (.[:-2] | rtrimstr(" ")) + " *" else . end)
    | (if endswith(" *") then {body: .[:-2], tail: "( .*)?"} else {body: ., tail: ""} end) as $p
    | ("^" + ($p.body | split("*") | map(esc) | join(".*")) + $p.tail + "$") as $re
    | select($c | test($re))
    | $rule' "$settings"
}

denied() {
  if [ -n "$(matches "$1")" ]; then
    echo "ok   $2 denies: $1"
  else
    echo "FAIL $2 does not deny: $1"
    fail=1
  fi
}

allowed() {
  local m
  m=$(matches "$1")
  if [ -z "$m" ]; then
    echo "ok   allowed: $1"
  else
    echo "FAIL denied by $(echo "$m" | tr '\n' ' '): $1"
    fail=1
  fi
}

# C29: the legacy suffix ":*" means "prefix", not a literal colon.
legacy=$(jq -r '.permissions.deny[] | select(endswith(":*)"))' "$settings")
if [ -z "$legacy" ]; then
  echo "ok   C29 no pattern ends in the legacy :* suffix"
else
  echo "FAIL C29 legacy :* suffix in: $legacy"
  fail=1
fi
has 'Bash(git push * :**)' C29
has 'Bash(git push :**)' C29

for p in 'git push -d*' 'git push * -d*' 'git push * --delete*' 'git push --prune*' \
  'git push * --prune*' 'git push --mirror*' 'git push * --mirror*' 'git push *-uf*' \
  'git -C * push*' 'git -c * push*' 'git push * +*' 'git tag --delete*' 'git tag * --delete*'; do
  has "Bash($p)" C27
done
for p in 'gh api -X*' 'gh api * -X*' 'gh api --method*' 'gh api * --method*' 'gh api * -f*' \
  'gh api * -F*' 'gh api * --field*' 'gh api * --raw-field*' 'gh api * --input*' 'gh api *graphql*'; do
  has "Bash($p)" C28
done
for p in 'git config alias.*' 'git config * alias.*' 'bash -c *' 'sh -c *' 'env * git push*' \
  'command git push*' 'gh repo archive*' 'gh repo rename*' 'gh workflow run*' 'gh workflow enable*' \
  'gh workflow disable*' 'gh run cancel*' 'gh secret*' 'gh variable*' 'gh label delete*' \
  'gh issue delete*' 'gh release *' 'gh ruleset*'; do
  has "Bash($p)" C30
done

denied 'git push -uf origin main' C27
denied 'git push origin -fu main' C27
denied 'git push -uvf origin main' C27
denied 'git push origin -d feature' C27
denied 'git push origin --delete feature' C27
denied 'git push --prune origin' C27
denied 'git push origin --mirror' C27
denied 'git -C /some/dir push origin main' C27
denied 'git -c push.default=current push' C27
denied 'git push upstream :feature' C27
denied 'git push https://example.invalid/r.git :feature' C27
denied 'git push :feature' C27
denied 'git push upstream +main' C27
denied 'git tag --delete v1.0.0' C27
denied 'gh api repos/x/y -X DELETE' C28
denied 'gh api -XDELETE repos/x/y' C28
denied 'gh api repos/x/y -XPUT' C28
denied 'gh api --method=PATCH repos/x/y' C28
denied 'gh api repos/x/y --method POST' C28
denied 'gh api -X POST repos/x/y/issues' C28
denied 'gh api repos/x/y/issues -f title=t' C28
denied 'gh api repos/x/y/issues -F title=t' C28
denied 'gh api repos/x/y/issues --field title=t' C28
denied 'gh api repos/x/y/issues --raw-field title=t' C28
denied 'gh api repos/x/y/issues --input body.json' C28
denied 'gh api graphql -f query=q' C28
denied 'git config alias.p push' C30
denied 'git config --global alias.p push' C30
denied 'bash -c "git push"' C30
denied 'sh -c "git push"' C30
denied 'env A=1 git push origin main' C30
denied 'command git push origin main' C30
denied 'gh repo archive x/y' C30
denied 'gh repo rename z' C30
denied 'gh workflow run ci.yml' C30
denied 'gh workflow disable ci.yml' C30
denied 'gh run cancel 123' C30
denied 'gh secret set X' C30
denied 'gh variable set X' C30
denied 'gh label delete bug' C30
denied 'gh issue delete 1' C30
denied 'gh ruleset list' C30

allowed 'git push'
allowed 'git push -u origin chore/claude-deny-and-hook'
allowed 'git push origin feat/fix-deps'
allowed 'git push origin HEAD:refs/heads/topic'
allowed 'gh pr create --fill'
allowed 'gh pr merge --auto --squash'
allowed 'gh pr merge 12 --auto --squash'
allowed 'gh pr comment 12 --body text'
allowed 'gh pr ready 12'
allowed 'gh pr close 12'
allowed 'gh issue create --title t --body b'
allowed 'gh issue edit 3 --add-label finding'
allowed 'gh issue comment 3 --body b'
allowed 'gh issue close 3'
allowed 'gh label create finding'
allowed 'gh run list'
allowed 'gh run view 123'
allowed 'gh run watch 123'
allowed 'gh run rerun 123'
allowed 'gh api repos/x/y'
allowed 'gh api repos/x/y --jq .z'
allowed 'gh api repos/x/y/pulls --paginate --jq .[].number'

exit "$fail"
