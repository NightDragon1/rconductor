#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Checks the deny list in .claude/settings.json (NFR-013, C27-C30, C36,
# C38, C43). It only
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
  'command git push*' 'gh repo archive*' 'gh repo rename*' 'gh workflow enable*' \
  'gh workflow disable*' 'gh run cancel*' 'gh secret*' 'gh variable*' 'gh label delete*' \
  'gh issue delete*' 'gh release *' 'gh ruleset*'; do
  has "Bash($p)" C30
done

for p in 'git config include.*' 'git config * include.*' 'git config includeIf.*' \
  'git config * includeIf.*' 'git config url.*' 'git config * url.*' 'git -c *include.*' \
  'git -c *remote.*' 'git --config-env*' 'git remote add*' 'git remote set-url*' 'gh alias set*' \
  'gh alias import*' 'gh repo sync --force*' 'gh repo sync * --force*' 'gh pr merge --admin*' \
  'gh pr merge * --admin*'; do
  has "Bash($p)" C36
done
for p in 'gh auth token*' 'gh ssh-key add*' 'gh repo deploy-key add*' 'gh extension install*' \
  'gh extensions install*' 'gh ext install*'; do
  has "Bash($p)" C38
done
for p in 'git commit *--no-verify*' 'git commit -n*' 'git commit * -n*' 'git * --no-verify*' \
  'git -c *core.hooksPath*' 'git config set *core.hooksPath*' 'git config unset *core.hooksPath*' \
  'git config --add *core.hooksPath*' 'git config * --add *core.hooksPath*' \
  'git config --replace-all *core.hooksPath*' 'git config * --replace-all *core.hooksPath*' \
  'git config --unset*core.hooksPath*' 'git config * --unset*core.hooksPath*'; do
  has "Bash($p)" C43
done
# File tools: Edit rules cover Edit, Write, MultiEdit and NotebookEdit.
for p in 'Edit(.git)' 'Edit(**/.git/**)' 'Edit(//**/.git)' 'Edit(//**/.git/**)' \
  'Edit(~/.gitconfig)' 'Edit(~/.config/git/**)' 'Edit(~/.cache/rconductor-claudeguard/**)' \
  'Edit(~/.config/rconductor-guard/**)'; do
  has "$p" C36
done
# The PreToolUse guard is registered for the shell and the file tools.
if jq -e '.hooks.PreToolUse[]? | select(.matcher == "Bash|Edit|Write|MultiEdit|NotebookEdit")
    | .hooks[] | select(.command | endswith("/.claude/hooks/guard.sh"))' "$settings" >/dev/null; then
  echo "ok   C36 PreToolUse guard is registered"
else
  echo "FAIL C36 PreToolUse guard is not registered"
  fail=1
fi

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
denied 'gh workflow disable ci.yml' C30
denied 'gh run cancel 123' C30
denied 'gh secret set X' C30
denied 'gh variable set X' C30
denied 'gh label delete bug' C30
denied 'gh issue delete 1' C30
denied 'gh ruleset list' C30
denied 'git config include.path /tmp/x.gitconfig' C36
denied 'git config --global includeIf.gitdir:/x/.path /tmp/x' C36
denied 'git config url.https://example.invalid/.insteadOf https://github.com/' C36
denied 'git -c include.path=/tmp/x status' C36
denied 'git -c remote.origin.mirror=true fetch' C36
denied 'git --config-env=alias.p=X p' C36
denied 'git remote add up https://example.invalid/r.git' C36
denied 'git remote set-url origin https://example.invalid/r.git' C36
denied "gh alias set p 'api -X DELETE repos/x/y'" C36
denied 'gh alias import aliases.yml' C36
denied 'gh repo sync x/y --source a/b --force' C36
denied 'gh repo sync --force' C36
denied 'gh pr merge 12 --admin --squash' C36
denied 'gh pr merge 12 --squash --admin=true' C36
denied 'gh auth token' C38
denied 'gh ssh-key add key.pub' C38
denied 'gh repo deploy-key add key.pub' C38
denied 'gh extension install owner/gh-ext' C38
denied 'gh ext install owner/gh-ext' C38
denied 'git commit --no-verify -m x' C43
denied 'git commit -n -m x' C43
denied 'git commit -m x -n' C43
denied 'git push --no-verify origin main' C43
denied 'git merge --no-verify origin/main' C43
denied 'git rebase --no-verify origin/main' C43
denied 'git pull --no-verify' C43
denied 'git -C /w commit --no-verify -m x' C43
denied 'git -c core.hooksPath=/dev/null commit -m x' C43
denied 'git config --add core.hooksPath /tmp/h' C43
denied 'git config --global --replace-all core.hooksPath /tmp/h' C43
denied 'git config --unset core.hooksPath' C43
denied 'git config --global --unset-all core.hooksPath' C43
denied 'git config set core.hooksPath /tmp/h' C43
denied 'git config unset core.hooksPath' C43

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
allowed 'git config --get remote.origin.url'
allowed 'git config core.hooksPath .githooks'
allowed 'git remote -v'
allowed 'git remote get-url origin'
allowed 'gh pr merge 12 --auto --squash --delete-branch'
allowed 'gh repo sync'
allowed 'gh auth status'
allowed 'gh extension list'
allowed 'gh alias list'
allowed 'git push -n origin main'
allowed 'git merge -n origin/main'
allowed 'git commit --amend --no-edit'
allowed 'git commit -m msg'
allowed 'git config --get core.hooksPath'
allowed 'git config --get-all core.hooksPath'
allowed 'git config core.hooksPath'
allowed 'git config --global core.hooksPath'
allowed 'git config --show-origin core.hooksPath'
allowed 'git config --list --show-origin'
allowed 'git config get core.hooksPath'

# gh workflow run is not in the deny list: a pattern cannot express the
# one configured exception, so the guard alone decides. Its decisions on a
# made-up configuration, nothing is executed.
allowed 'gh workflow run ci.yml --ref main'
repo=$(cd "$(dirname "$0")/.." && pwd)
gtmp=$(mktemp -d)
trap 'rm -rf "$gtmp"' EXIT
go -C "$repo/tools" build -o "$gtmp/guard" ./claudeguard
sha=0123456789abcdef0123456789abcdef01234567
form="gh workflow run test.yml -R owner/repo-x -f ref="
# guard_says <want allow|deny> <label> <config dir> <command>
guard_says() {
  local got=allow
  jq -nc --arg c "$4" '{tool_name: "Bash", cwd: "/w", tool_input: {command: $c}}' |
    HOME="$gtmp/home" XDG_CONFIG_HOME="$3" XDG_CACHE_HOME="$gtmp/cache" "$gtmp/guard" 2>/dev/null || got=deny
  if [ "$got" = "$1" ]; then echo "ok   $2"; else echo "FAIL $2: guard says $got"; fail=1; fi
}
mkdir -p "$gtmp/conf/rconductor-guard"
echo "test.yml -R owner/repo-x -f ref=<sha>" >"$gtmp/conf/rconductor-guard/workflows"
guard_says deny "C30 guard denies release.yml" "$gtmp/conf" "gh workflow run release.yml --ref main"
guard_says deny "C30 guard denies a short ref" "$gtmp/conf" "$form${sha:0:39}"
guard_says deny "C30 guard denies a ref that is not hex" "$gtmp/conf" "${form}g${sha:1}"
guard_says deny "C30 guard denies the form without configuration" "$gtmp/none" "$form$sha"
guard_says allow "C30 guard allows the configured form" "$gtmp/conf" "$form$sha"

exit "$fail"
