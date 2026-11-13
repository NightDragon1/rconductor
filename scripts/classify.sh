#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Review stage of a pull request (PRJ-005), from its changed files and the
# class file .github/core-paths.yml:
#   classify.sh CLASS_FILE [FILE_LIST]      (FILE_LIST from stdin if left out)
# FILE_LIST has one line per changed file, fields separated by a tab:
#   status  additions  deletions  filename  previous_filename
# as jq's @tsv makes it from the GitHub list of pull request files (control
# characters in names come escaped; that never adds or removes a "/").
# Prints GITHUB_OUTPUT lines:
#   stage   stage-2 if a file that is not generated matches a core glob by
#           its name or its previous name (rename, deletion), else stage-1
#   areas   the matched areas, comma-separated
#   lines   additions plus deletions, without generated files
# Exit 1: a stage 2 pull request over 400 changed lines (the outputs are
# printed first). Exit 2: bad arguments, class file or file list.
set -euo pipefail
export LC_ALL=C
limit=400

die() { echo "classify: $*" >&2; exit 2; }
[[ $# -ge 1 && $# -le 2 ]] || die "usage: classify.sh CLASS_FILE [FILE_LIST]"
class=$1 list=${2:-/dev/stdin}

# glob_re turns a glob into an anchored extended regular expression.
glob_re() {
  local g=$1 re= i c
  for ((i = 0; i < ${#g}; i++)); do
    c=${g:i:1}
    if [[ $c == '*' && ${g:i+1:1} == '*' ]]; then
      if [[ ${g:i+2:1} == / ]]; then
        re+='(.*/)?' i=$((i + 2))
      else
        re+='.*' i=$((i + 1))
      fi
    elif [[ $c == '*' ]]; then
      re+='[^/]*'
    elif [[ $c == '?' ]]; then
      re+='[^/]'
    elif [[ $c == [[:alnum:]_/-] ]]; then
      re+=$c
    elif [[ $c == '\' ]]; then
      re+='[\]'
    elif [[ $c == '^' ]]; then
      re+='\^'
    else
      re+="[$c]"
    fi
  done
  printf '^%s$' "$re"
}

areas=() res=() gen=()
area= n=0
while IFS= read -r line || [[ -n $line ]]; do
  n=$((n + 1))
  [[ -z $line || $line == '#'* ]] && continue
  if [[ $line =~ ^([a-z][a-z0-9_-]*):$ ]]; then
    area=${BASH_REMATCH[1]}
  elif [[ -n $area && $line =~ ^\ \ -\ (.+)$ ]]; then
    glob=${BASH_REMATCH[1]}
    [[ $glob =~ ^\"(.+)\"$ ]] && glob=${BASH_REMATCH[1]}
    [[ $glob == '*'* || $glob == *[[:space:]]* ]] && die "$class:$n: bad glob: $glob"
    if [[ $area == generated ]]; then
      gen+=("$(glob_re "$glob")")
    else
      areas+=("$area") res+=("$(glob_re "$glob")")
    fi
  else
    die "$class:$n: unexpected line"
  fi
done <"$class"
((${#res[@]})) || die "$class: no core globs"

generated() {
  local re
  for re in "${gen[@]}"; do [[ $1 =~ $re ]] && return 0; done
  return 1
}

declare -A hit=()
lines=0 n=0
while IFS=$'\t' read -r status add del name prev || [[ -n ${status:-} ]]; do
  n=$((n + 1))
  [[ -n $status && -n $name && $add =~ ^[0-9]+$ && $del =~ ^[0-9]+$ ]] ||
    die "file list line $n: expected status, additions, deletions, filename"
  counted=false
  for p in "$name" ${prev:+"$prev"}; do
    generated "$p" && continue
    counted=true
    for i in "${!res[@]}"; do
      [[ $p =~ ${res[i]} ]] && hit[${areas[i]}]=1
    done
  done
  # Lines count unless every name of the file is generated, so a move
  # into a generated directory still counts.
  $counted && lines=$((lines + 10#$add + 10#$del))
done <"$list"

stage=stage-1
((${#hit[@]})) && stage=stage-2
matched=
for a in $(printf '%s\n' "${!hit[@]}" | sort); do matched+=${matched:+,}$a; done
echo "stage=$stage"
echo "areas=$matched"
echo "lines=$lines"
if [[ $stage == stage-2 ]] && ((lines > limit)); then
  echo "classify: stage 2 pull request with $lines changed lines (generated files excluded); the limit is $limit, split it" >&2
  exit 1
fi
