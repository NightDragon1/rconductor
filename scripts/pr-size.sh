#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Size of a pull request (PRJ-005): counts the changed lines without the
# generated files of the class file and prints the label arguments for
# gh pr edit. It only informs: over the limit adds the label over-limit,
# within removes it; it never fails a run.
#   pr-size.sh CLASS_FILE FILE_LIST < CURRENT_LABELS
# FILE_LIST: one line per file, tab-separated: status, additions, deletions,
# filename, previous filename (may be empty), as the pulls/files API lists.
# Output: "lines=N", "size=within|over", then one label argument per line.
set -uo pipefail

limit=400
class=${1-} list=${2-}
[[ -r $class && -r $list ]] || { echo "pr-size: usage: pr-size.sh CLASS_FILE FILE_LIST < LABELS" >&2; exit 2; }

# glob_re turns a glob of the class file into an anchored regular expression.
glob_re() {
  local g=$1 re= i c
  for ((i = 0; i < ${#g}; i++)); do
    c=${g:i:1}
    if [[ $c == '*' && ${g:i+1:1} == '*' ]]; then
      if [[ ${g:i+2:1} == / ]]; then re+='(.*/)?' i=$((i + 2)); else re+='.*' i=$((i + 1)); fi
    elif [[ $c == '*' ]]; then
      re+='[^/]*'
    elif [[ $c == '?' ]]; then
      re+='[^/]'
    elif [[ $c == [[:alnum:]_/-] ]]; then
      re+=$c
    else
      re+="[$c]"
    fi
  done
  printf '^%s$' "$re"
}

gen=() area=
while IFS= read -r line || [[ -n $line ]]; do
  if [[ $line =~ ^([a-z][a-z0-9_-]*):$ ]]; then
    area=${BASH_REMATCH[1]}
  elif [[ $area == generated && $line =~ ^\ \ -\ \"?([^\"]+)\"?$ ]]; then
    gen+=("$(glob_re "${BASH_REMATCH[1]}")")
  fi
done <"$class"

generated() {
  local re
  for re in "${gen[@]}"; do [[ $1 =~ $re ]] && return 0; done
  return 1
}

lines=0
while IFS=$'\t' read -r _ add del name prev || [[ -n ${name:-} ]]; do
  [[ -n ${name:-} && $add =~ ^[0-9]+$ && $del =~ ^[0-9]+$ ]] || continue
  # A move into a generated file still counts.
  if ! generated "$name" || { [[ -n ${prev:-} ]] && ! generated "$prev"; }; then
    lines=$((lines + 10#$add + 10#$del))
  fi
done <"$list"

size=within
((lines > limit)) && size=over
echo "lines=$lines"
echo "size=$size"
labels=$(cat)
has() { printf '%s\n' "$labels" | grep -qxF "$1"; }
if [[ $size == over ]]; then
  has over-limit || printf '%s\n' --add-label over-limit
else
  has over-limit && printf '%s\n' --remove-label over-limit
fi
# The label of the earlier size check is gone with it.
has over-limit-tests && printf '%s\n' --remove-label over-limit-tests
exit 0
