#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Synthetic upgrade test (NFR-013, SRV-008, AGT-013): starts an old panel and
# agent on their own volumes, fills them with test data through the old API,
# replaces the panel with the new image on the same volumes while the old
# agent stays connected, checks migration, sign-in, data and agent, then
# updates the agent too. See test/upgrade/README.md.
#
#   test/upgrade/run.sh OLD_PANEL OLD_AGENT NEW_PANEL NEW_AGENT
#
# All passwords and tokens are generated at run time and stay in a private
# temporary directory; output is masked. Exit code 0 only if every check
# passes.
set -uo pipefail

if [ $# -ne 4 ]; then
  echo "usage: $0 OLD_PANEL_IMAGE OLD_AGENT_IMAGE NEW_PANEL_IMAGE NEW_AGENT_IMAGE" >&2
  exit 2
fi
OLD_PANEL=$1 OLD_AGENT=$2 NEW_PANEL=$3 NEW_AGENT=$4
REPO=$(cd "$(dirname "$0")/../.." && pwd)
PROJECT=${RCD_UPGRADE_PROJECT:-rcdupgrade$$}
KEEP=${RCD_UPGRADE_KEEP:-0}
DOCKER_GID=${RCD_UPGRADE_DOCKER_GID:-$(stat -c %g /var/run/docker.sock)}
WAIT=${RCD_UPGRADE_WAIT:-90}
# Every step is bounded: STEP for one docker or go command, PULL for an
# image pull, DEADLINE for the whole run (then it stops and cleans up).
STEP=${RCD_UPGRADE_STEP_TIMEOUT:-300}
PULL=${RCD_UPGRADE_PULL_TIMEOUT:-600}
DEADLINE=${RCD_UPGRADE_DEADLINE:-1800}
# The whole run ends after DEADLINE seconds: timeout sends TERM, the trap
# below cleans up.
if [ -z "${RCD_UPGRADE_BOUNDED:-}" ]; then
  RCD_UPGRADE_BOUNDED=1 exec timeout --signal=TERM --kill-after=60 "$DEADLINE" "$0" "$@"
fi

for tool in docker curl jq go; do
  command -v "$tool" >/dev/null || { echo "missing tool: $tool" >&2; exit 2; }
done

# The schema version the new panel must reach: the highest migration number in
# this checkout, unless given.
expected_schema() {
  {
    ls "$REPO"/migrations/sqlite/ | sed -n 's/^0*\([0-9][0-9]*\)_.*\.sql$/\1/p'
    sed -n 's/.*NewGoMigration(\([0-9][0-9]*\),.*/\1/p' "$REPO"/migrations/sqlite_go.go
  } | sort -n | tail -n 1
}
EXPECT_SCHEMA=${RCD_UPGRADE_SCHEMA:-$(expected_schema)}

port_free() { ! (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }
PORT=${RCD_UPGRADE_PORT:-}
if [ -z "$PORT" ]; then
  for p in $(seq 18180 18280); do
    if port_free "$p"; then PORT=$p; break; fi
  done
fi
[ -n "$PORT" ] && port_free "$PORT" || { echo "no free port" >&2; exit 2; }
B=http://127.0.0.1:$PORT

# The work directory lives in the ignored scratch directory of the clone
# (.cache/upgrade), not in a shared temporary directory.
mkdir -p "$REPO/.cache/upgrade"
WORK=$(mktemp -d "$REPO/.cache/upgrade/run.XXXXXX")
chmod 700 "$WORK"
mkdir -p "$WORK/sec" "$WORK/jar" "$WORK/out" "$WORK/snap" "$WORK/bin"

dc() { timeout "$STEP" docker compose -p "$PROJECT" -f "$WORK/compose.yaml" --env-file "$WORK/sec/env" "$@"; }

cleanup() {
  status=$?
  if [ "$KEEP" = 1 ]; then
    echo "kept: compose project $PROJECT, work dir $WORK" >&2
  else
    dc down -v --remove-orphans --timeout 10 >/dev/null 2>&1
    rm -rf "$WORK"
  fi
  exit $status
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

# --- output -----------------------------------------------------------------

mask() {
  sed -E \
    -e 's/rcd1[.A-Za-z0-9_~-]*/rcd1***/g' \
    -e 's/("(password|token|new_password|current_password|url)" *: *")[^"]*"/\1***"/g' \
    -e 's/((password|token|secret)=)[^ ]+/\1***/g' \
    -e 's/(password: +)[^ ]+/\1***/g'
}
log() { printf '%s %s\n' "$(date -u +%H:%M:%S)" "$*" | mask; }
die() {
  log "FATAL: $*"
  if [ "$BASHPID" != "$$" ]; then exit 1; fi # in $(...): the caller checks
  summary
  exit 1
}
# id NAME VALUE: dies unless VALUE looks like an ID (results of $(...))
need_id() { case $2 in '' | null) die "$1 failed" ;; esac; echo "$2"; }

RESULTS=$WORK/results
: >"$RESULTS"
# check NAME EXPECTED RESULT OK(0|1)
check() {
  local st=FAIL
  [ "$4" = 0 ] && st=PASS
  printf '%s\t%s\t%s\t%s\n' "$st" "$1" "$(echo "$2" | tr '\t\n' '  ')" "$(echo "$3" | tr '\t\n' '  ')" | mask >>"$RESULTS"
  log "$st: $1 - $3"
}
# expect NAME EXPECTED ACTUAL: passes when both are equal
expect() { [ "$2" = "$3" ]; check "$1" "$2" "$3" $?; }

summary() {
  echo
  printf '%-6s | %-48s | %-40s | %s\n' status check expected result
  printf '%s\n' "-------+--------------------------------------------------+------------------------------------------+-------"
  while IFS="$(printf '\t')" read -r st name exp res; do
    printf '%-6s | %-48s | %-40s | %s\n' "$st" "$name" "$exp" "$res"
  done <"$RESULTS"
  echo
  echo "passed: $(grep -c '^PASS' "$RESULTS"), failed: $(grep -c '^FAIL' "$RESULTS")"
}

# --- API --------------------------------------------------------------------

# call JAR METHOD PATH [BODY] [IF-MATCH]; code in $CODE, body in $WORK/out/body
CODE=
call() {
  local jar=$1 m=$2 p=$3 body=${4:-} etag=${5:-}
  local args=(-s -m 30 -o "$WORK/out/body" -D "$WORK/out/head" -w '%{http_code}' -X "$m"
    -b "$WORK/jar/$jar" -c "$WORK/jar/$jar" -H "Origin: $B" -H 'Content-Type: application/json')
  [ -n "$body" ] && args+=(--data-binary "$body")
  [ -n "$etag" ] && args+=(-H "If-Match: $etag")
  CODE=$(curl "${args[@]}" "$B/api/v1$p")
}
out() { cat "$WORK/out/body"; }
etag() { sed -n 's/^[Ee][Tt][Aa][Gg]: *//p' "$WORK/out/head" | tr -d '\r'; }
# must CODE...: dies unless the last call answered one of the codes
must() {
  local c
  for c in "$@"; do [ "$CODE" = "$c" ] && return 0; done
  die "$LAST_STEP: HTTP $CODE $(head -c 400 "$WORK/out/body")"
}
step() { LAST_STEP=$*; }

# change JAR METHOD PATH BODY: GET the object for its ETag, then change it
change() {
  call "$1" GET "$3"
  local tag
  tag=$(etag)
  if [ -z "$tag" ]; then
    tag=$(out | jq -r '.version // empty' 2>/dev/null)
    [ -n "$tag" ] && tag="\"$tag\""
  fi
  call "$1" "$2" "$3" "$4" "$tag"
}

newpw() { head -c 24 /dev/urandom | base64 | tr -d '/+=\n' | head -c 24 >"$WORK/sec/$1.pw"; }
login() { # login JAR USER
  rm -f "$WORK/jar/$1"
  call "$1" POST /auth/login "$(jq -nc --arg u "$2" --rawfile p "$WORK/sec/$2.pw" '{pseudonym:$u,password:$p}')"
}

wait_http() { # wait until the panel answers
  local i code
  for i in $(seq 1 "$WAIT"); do
    code=$(curl -s -m 2 -o /dev/null -w '%{http_code}' "$B/api/v1/auth/session")
    [ "$code" = 401 ] && return 0
    sleep 1
  done
  return 1
}

redeem() { # redeem USER (token in $WORK/sec/invite)
  newpw "$1"
  call none POST /invites/redeem "$(jq -nc --arg u "$1" --rawfile t "$WORK/sec/invite" --rawfile p "$WORK/sec/$1.pw" '{token:$t,pseudonym:$u,password:$p}')"
  must 201
  out | jq -r .id
}
invite() { # invite JAR ROLE USER -> prints user id
  step "invite $3 as $2"
  call "$1" POST /invites "{\"role\":\"$2\"}"
  must 201
  out | jq -r .token | tr -d '\n' >"$WORK/sec/invite"
  redeem "$3"
}

# --- stack ------------------------------------------------------------------

go_arch=$(docker version --format '{{.Server.Arch}}')
log "building helpers for linux/$go_arch"
(cd "$REPO" && CGO_ENABLED=0 GOOS=linux GOARCH=$go_arch timeout "$STEP" go build -trimpath -o "$WORK/bin/rconsrv" ./test/upgrade/rconsrv &&
  CGO_ENABLED=0 GOOS=linux GOARCH=$go_arch timeout "$STEP" go build -trimpath -o "$WORK/bin/dbstat" ./test/upgrade/dbstat) || die "go build"
chmod 755 "$WORK" "$WORK/bin" "$WORK/bin/"*

head -c 18 /dev/urandom | base64 | tr -d '/+=\n' >"$WORK/sec/rcon.pw"
write_env() { # write_env PANEL AGENT
  umask 077
  {
    echo "PANEL_IMAGE=$1"
    echo "AGENT_IMAGE=$2"
    echo "RCON_IMAGE=$OLD_PANEL"
    echo "PORT=$PORT"
    echo "DOCKER_GID=$DOCKER_GID"
    echo "RCON_PASSWORD=$(cat "$WORK/sec/rcon.pw")"
    [ -f "$WORK/sec/pairing" ] && echo "RCD_PAIRING_TOKEN=$(cat "$WORK/sec/pairing")"
  } >"$WORK/sec/env"
}
cat >"$WORK/compose.yaml" <<EOF
services:
  panel:
    image: \${PANEL_IMAGE}
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    mem_limit: 1g
    ports: ["127.0.0.1:\${PORT}:8080"]
    environment:
      RCD_AGENT_PUBLIC_ADDR: panel:8443
    volumes: [data:/data, secrets:/secrets, backups:/backups]
  agent:
    image: \${AGENT_IMAGE}
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    mem_limit: 1g
    group_add: ["\${DOCKER_GID}"]
    environment:
      RCD_PAIRING_TOKEN: \${RCD_PAIRING_TOKEN:-}
      RCD_AGENT_NAME: upgrade-agent
    volumes: [/var/run/docker.sock:/var/run/docker.sock, agent-secrets:/secrets]
  rcon:
    image: \${RCON_IMAGE}
    entrypoint: ["/rconsrv"]
    labels: {rconductor.manage: "true"}
    environment:
      RCON_PASSWORD: \${RCON_PASSWORD}
    volumes: ["$WORK/bin/rconsrv:/rconsrv:ro"]
volumes:
  data:
  secrets:
  backups:
  agent-secrets:
EOF

# dbstat ARGS...: the helper in a container on the data volumes (panel stopped)
dbstat() {
  timeout "$STEP" docker run --rm --network none --label "com.docker.compose.project=$PROJECT" \
    -v "${PROJECT}_data:/data" -v "${PROJECT}_backups:/backups:ro" \
    -v "$WORK/bin/dbstat:/dbstat:ro" --entrypoint /dbstat "$OLD_PANEL" "$@"
}

for img in "$OLD_PANEL" "$OLD_AGENT" "$NEW_PANEL" "$NEW_AGENT"; do
  docker image inspect "$img" >/dev/null 2>&1 || timeout "$PULL" docker pull -q "$img" >/dev/null || die "image $img"
  log "image $img: $(docker image inspect "$img" --format '{{.Id}} version={{index .Config.Labels "org.opencontainers.image.version"}}')"
done

# --- 1. old panel and agent -------------------------------------------------

write_env "$OLD_PANEL" "$OLD_AGENT"
log "project $PROJECT on port $PORT; starting old panel"
dc up -d rcon panel >/dev/null 2>&1 || die "compose up"
wait_http || die "old panel does not answer"
dc logs --no-color panel 2>&1 | sed -n 's/.*|[[:space:]]*password:[[:space:]]*\([^[:space:]]*\).*/\1/p' | head -n 1 | tr -d '\n' >"$WORK/sec/admin.pw"
[ -s "$WORK/sec/admin.pw" ] || die "no initial password in the log"
OLD_VERSION=$(dc logs --no-color panel 2>&1 | sed -n 's/.*event=app.start version=\([^ ]*\).*/\1/p' | head -n 1)
log "old panel version $OLD_VERSION"

# --- 2. test data -----------------------------------------------------------

step "admin first login"
login admin admin
must 200
newpw admin.new
call admin POST /me/password "$(jq -nc --rawfile c "$WORK/sec/admin.pw" --rawfile n "$WORK/sec/admin.new.pw" '{current_password:$c,new_password:$n}')"
must 204
mv "$WORK/sec/admin.new.pw" "$WORK/sec/admin.pw"
login admin admin
must 200
ADMIN_ID=$(out | jq -r .user.id)

step profiles
call admin GET /profiles
must 200
P1=$(out | jq -r '.items[0].id')
P2=$(out | jq -r '.items[1].id // .items[0].id')

server() { # server NAME PROFILE HOST -> id
  step "server $1"
  local b
  b=$(jq -nc --arg n "$1" --arg p "$2" --arg h "$3" --rawfile pw "$WORK/sec/rcon.pw" '{name:$n,profile_id:$p,host:$h,port:25575,password:$pw}')
  call admin POST /servers "$b"
  if [ "$CODE" != 201 ]; then # a release with profile lists
    call admin POST /servers "$(echo "$b" | jq -c '.profile_ids=[.profile_id] | del(.profile_id)')"
  fi
  must 201
  out | jq -r .id
}
SA=$(server srv-a "$P1" rcon)
SA=$(need_id SA "$SA") || exit 1
SB=$(server srv-b "$P2" rcon)
SB=$(need_id SB "$SB") || exit 1
SC=$(server srv-c "$P1" rcon)
SC=$(need_id SC "$SC") || exit 1
SD=$(server srv-dead "$P1" rcon-missing.invalid)
SD=$(need_id SD "$SD") || exit 1

step "server groups"
call admin POST /server-groups "$(jq -nc --arg a "$SA" --arg b "$SB" '{name:"grp-ab",server_ids:[$a,$b]}')"
must 201
GAB=$(out | jq -r .id)
call admin POST /server-groups "$(jq -nc --arg d "$SD" '{name:"grp-dead",server_ids:[$d]}')"
must 201

step "supporters on"
change admin PATCH /settings '{"supporters.enabled":true}'
must 200 204

U_SA=$(invite admin server_admin sa1)
U_SA=$(need_id U_SA "$U_SA") || exit 1
step "scope sa1"
change admin PUT "/users/$U_SA/assignments" "$(jq -nc --arg g "$GAB" --arg c "$SC" --arg d "$SD" '{admin_scopes:[{server_group_id:$g},{server_id:$c},{server_id:$d}]}')"
must 200 204
step "general group"
call admin POST /user-groups '{"name":"staff","kind":"general"}'
must 201
G_STAFF=$(out | jq -r .id)
change admin PATCH "/user-groups/$G_STAFF" "$(jq -nc --arg u "$U_SA" '{members:[$u]}')"
must 200 204

step "sa1 setup login"
login sa sa1
must 200
step "command list"
call sa POST /command-lists '{"name":"mods","entries":[{"path":"list","passable":true},{"path":"whitelist add","passable":false}]}'
must 201
CL=$(out | jq -r .id)
step "moderator group"
call sa POST /user-groups "$(jq -nc --arg l "$CL" '{name:"mods-a",kind:"moderator",command_list_id:$l}')"
must 201
G_MOD=$(out | jq -r .id)
U_MOD=$(invite sa moderator mod1)
U_MOD=$(need_id U_MOD "$U_MOD") || exit 1
U_MOD2=$(invite sa moderator mod2)
U_MOD2=$(need_id U_MOD2 "$U_MOD2") || exit 1
step "mod1 release and grants"
change sa PATCH "/users/$U_MOD" '{"supporters_allowed":true}'
must 200 204
change sa PUT "/users/$U_MOD/assignments" "$(jq -nc --arg m "$G_MOD" --arg g "$GAB" '{groups:[$m],grants:[{group_id:$m,server_group_id:$g}]}')"
must 200 204
step "mod2 locked"
change sa PATCH "/users/$U_MOD2" '{"disabled":true}'
must 200 204

step "mod1 setup login"
login mod mod1
must 200
U_SUP=$(invite mod supporter sup1)
U_SUP=$(need_id U_SUP "$U_SUP") || exit 1
step "sup1 grant"
change mod PUT "/users/$U_SUP/assignments" "$(jq -nc --arg m "$U_MOD" --arg s "$SA" '{supporter_grants:[{moderator_id:$m,server_id:$s}]}')"
must 200 204

step "pairing token"
call admin POST /agents/tokens
must 201
out | jq -r .token | tr -d '\n' >"$WORK/sec/pairing"
write_env "$OLD_PANEL" "$OLD_AGENT"
dc up -d agent >/dev/null 2>&1 || die "agent up"
agent_up() { # agent_up VERSION-PATTERN: wait for a connected, active agent
  local i
  for i in $(seq 1 "$WAIT"); do
    call admin GET /agents
    if out | jq -e --arg v "$1" '.items[] | select(.status=="active" and .connected and (.version|test($v)))' >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}
agent_up . || die "old agent does not connect"
AGENT_ID=$(out | jq -r '.items[0].id')
RCON_CONTAINER=${PROJECT}-rcon-1
step "link srv-a to the agent"
change admin PATCH "/servers/$SA" "$(jq -nc --arg a "$AGENT_ID" --arg c "$RCON_CONTAINER" '{agent_id:$a,container_name:$c}')"
must 200

step favorites
call admin PUT /me/favorites "$(jq -nc --arg a "$SA" --arg b "$SB" '{server_ids:[$a,$b]}')"
must 200 204
call mod PUT /me/favorites "$(jq -nc --arg a "$SA" '{server_ids:[$a]}')"
must 200 204

# Sessions that must survive the update: one per role, two for the admin.
for u in "admin admin" "admin2 admin" "sa sa1" "mod mod1" "sup sup1"; do
  set -- $u
  step "login $2"
  login "$1" "$2"
  must 200
done

wait_connected() { # wait_connected SERVER
  local i
  for i in $(seq 1 "$WAIT"); do
    call admin GET "/servers/$1"
    [ "$(out | jq -r .status)" = connected ] && return 0
    sleep 1
  done
  return 1
}
wait_connected "$SA" || die "srv-a does not connect over RCON"

cmd() { # cmd JAR SERVER COMMAND -> "HTTP result"
  call "$1" POST "/servers/$2/commands" "$(jq -nc --arg c "$3" '{command:$c}')"
  echo "$CODE $(out | jq -r '.result // .reason // "-"' 2>/dev/null)"
}
log "commands: admin list $(cmd admin "$SA" list); admin say $(cmd admin "$SB" 'say hello')"
log "commands: admin dead $(cmd admin "$SD" list); sa1 $(cmd sa "$SC" list); sa1 $(cmd sa "$SA" 'whitelist add bob')"
log "commands: mod1 $(cmd mod "$SA" list); mod1 op $(cmd mod "$SA" 'op mallory'); mod1 $(cmd mod "$SB" list)"
log "commands: sup1 $(cmd sup "$SA" list); sup1 whitelist $(cmd sup "$SA" 'whitelist add eve')"

# --- snapshots --------------------------------------------------------------

pages() { # pages JAR PATH: all items of a paged list
  local cursor= sep n=0
  : >"$WORK/out/pages"
  while :; do
    n=$((n + 1))
    # A list with a cursor that never ends stops after 100 pages.
    [ "$n" -le 100 ] || { echo "more than 100 pages" >>"$WORK/out/pages"; break; }
    sep='?'
    case $2 in *\?*) sep='&' ;; esac
    call "$1" GET "$2${sep}limit=200${cursor:+&cursor=$cursor}"
    [ "$CODE" = 200 ] || { echo "HTTP $CODE" >>"$WORK/out/pages"; break; }
    out | jq -c '.items[]' >>"$WORK/out/pages"
    cursor=$(out | jq -r '.next_cursor // empty')
    [ -n "$cursor" ] || break
  done
  jq -s -c . "$WORK/out/pages"
}
snapshot() { # snapshot DIR
  local d=$WORK/snap/$1 u
  mkdir -p "$d"
  call admin GET /users
  out | jq -S '[.items[] | {id,pseudonym,role,disabled,supporters_allowed,is_emergency,created_at}] | sort_by(.id)' >"$d/users"
  for u in "$U_SA" "$U_MOD" "$U_MOD2" "$U_SUP"; do
    call admin GET "/users/$u/assignments"
    out | jq -S -c '{role,admin_scopes,groups,grants,supporter_grants} | map_values(if type=="array" then sort_by(tostring) else . end)'
  done >"$d/assignments"
  call admin GET /servers
  out | jq -S '[.items[] | {id,name,protocol,host,port,credentials,agent_id,container_name,game_ports,
      groups:(.groups|sort), profiles:(.profile_ids // ([.profile_id]|map(select(. != null))))}] | sort_by(.id)' >"$d/servers"
  out | jq -S '[.items[] | {id,version}] | sort_by(.id)' >"$d/versions"
  call admin GET /server-groups
  out | jq -S '[.items[] | del(.version) | .server_ids|=sort] | sort_by(.id)' >"$d/server-groups"
  call admin GET /user-groups
  out | jq -S '[.items[] | del(.version)] | sort_by(.id)' >"$d/user-groups"
  call admin GET /command-lists
  out | jq -S '[.items[] | del(.version)] | sort_by(.id)' >"$d/command-lists"
  for u in "$SA" "$SB" "$SC" "$SD"; do
    pages admin "/servers/$u/history" | jq -c --arg s "$u" 'map({server:$s,request_id,ts,user_id,pseudonym,role,command,result,reason}) | sort_by(.request_id)[]'
  done >"$d/history"
  pages admin /audit | jq -c 'map({id,ts,kind,action,actor_id,actor_role,server_id,target_type,target_id,result,reason}) | sort_by(.id)[]' >"$d/audit"
  for u in admin mod; do
    call "$u" GET /me/favorites
    echo "$u $(out | jq -c .server_ids)"
  done >"$d/favorites"
  for u in admin sa mod sup; do
    call "$u" GET /servers
    echo "$u $(out | jq -c '[.items[].id] | sort')"
  done >"$d/scope"
  call admin GET /settings
  out | jq -S . >"$d/settings"
  call admin GET /agents
  out | jq -S '[.items[] | {id,name,fingerprint,status,paired_by,created_at}]' >"$d/agents"
}

step "snapshot before"
snapshot before
B_HIST=$(wc -l <"$WORK/snap/before/history")
B_AUDIT=$(wc -l <"$WORK/snap/before/audit")
log "before: $(jq length "$WORK/snap/before/users") managed users, $(jq length "$WORK/snap/before/servers") servers, $B_HIST history entries, $B_AUDIT audit entries"

# --- 3. stop the old panel, legacy data, counts ------------------------------

log "stopping the old panel"
dc stop panel >/dev/null 2>&1
dbstat counts /data/rconductor.db >"$WORK/snap/before/tables" || die "dbstat counts"
OLD_SCHEMA=$(dbstat query /data/rconductor.db 'SELECT MAX(version_id) FROM goose_db_version')
# A server without profile, as older databases can hold it (the API of 1.0
# requires one).
if dbstat query /data/rconductor.db "SELECT name FROM pragma_table_info('servers') WHERE name='profile_id'" | grep -q profile_id; then
  dbstat exec /data/rconductor.db "UPDATE servers SET profile_id = NULL WHERE id = '$SC'" >/dev/null || die "null profile"
  # Expected from that UPDATE: no profile, so no game ports from one, and the
  # version trigger of the old schema raises the version once.
  jq -S --arg c "$SC" 'map(if .id==$c then .profiles=[] | .game_ports=[] else . end)' "$WORK/snap/before/servers" >"$WORK/snap/before/x" &&
    mv "$WORK/snap/before/x" "$WORK/snap/before/servers"
  jq -S --arg c "$SC" 'map(if .id==$c then .version=(.version|tonumber+1|tostring) else . end)' "$WORK/snap/before/versions" >"$WORK/snap/before/x" &&
    mv "$WORK/snap/before/x" "$WORK/snap/before/versions"
fi

# --- 4. new panel, old agent ------------------------------------------------

write_env "$NEW_PANEL" "$OLD_AGENT"
log "starting the new panel on the same volumes"
dc up -d --no-deps panel >/dev/null 2>&1 || die "new panel up"
wait_http || die "new panel does not answer"
PLOG=$WORK/out/panel-new.log
dc logs --no-color panel >"$PLOG" 2>&1
NEW_VERSION=$(sed -n 's/.*event=app.start version=\([^ ]*\).*/\1/p' "$PLOG" | tail -n 1)
mig_start=$(grep 'event=db.migrate.start' "$PLOG" | tail -n 1 | grep -o 'from=[0-9]* to=[0-9]*')
mig_done=$(grep 'event=db.migrate.done' "$PLOG" | tail -n 1 | grep -o 'version=[0-9]*')
expect "migration start (old schema -> new)" "from=$OLD_SCHEMA to=$EXPECT_SCHEMA" "$mig_start"
expect "migration done" "version=$EXPECT_SCHEMA" "$mig_done"
bk=$(dbstat ls /backups | grep -c -- "-pre-migrate-v$OLD_SCHEMA\.db$")
expect "backup before migration in /backups" "1 pre-migrate-v$OLD_SCHEMA file" "$bk pre-migrate-v$OLD_SCHEMA file"

# Old sessions: the update itself ends none (docs/user/roles.md lists what
# ends a session, an update is not among it).
for j in admin admin2 sa mod sup; do
  cp "$WORK/jar/$j" "$WORK/jar/$j.old"
  call "$j.old" GET /auth/session
  expect "session from old panel still valid ($j)" 200 "$CODE"
done
for u in "admin admin rcon_admin" "sa sa1 server_admin" "mod mod1 moderator" "sup sup1 supporter"; do
  set -- $u
  login "$1" "$2"
  expect "password login $2" "200 $3" "$CODE $(out | jq -r .user.role)"
done
login mod2 mod2
[ "$CODE" != 200 ]
check "locked user stays locked (mod2)" "no session" "HTTP $CODE" $?
[ -n "$NEW_VERSION" ] && [ "$NEW_VERSION" != "$OLD_VERSION" ]
check "new panel reports another version" "not $OLD_VERSION" "$NEW_VERSION" $?

step "snapshot after"
snapshot after
for f in users assignments servers server-groups user-groups command-lists favorites scope agents; do
  if diff -u "$WORK/snap/before/$f" "$WORK/snap/after/$f" >"$WORK/out/diff" 2>&1; then
    check "data unchanged: $f" equal equal 0
  else
    check "data unchanged: $f" equal "differs: $(grep -E '^[-+][^-+]' "$WORK/out/diff" | head -n 6 | tr -s ' \n' ' ')" 1
  fi
done
# New keys are fine; the old ones keep their value.
changed=$(jq -rn --slurpfile a "$WORK/snap/before/settings" --slurpfile b "$WORK/snap/after/settings" \
  '[$a[0] | to_entries[] | select($b[0][.key] != .value) | .key] | join(",")')
expect "data unchanged: settings (keys of the old panel)" "" "$changed"
vdiff=$(jq -rn --slurpfile a "$WORK/snap/before/versions" --slurpfile b "$WORK/snap/after/versions" \
  '[$a[0][] as $x | $b[0][] | select(.id==$x.id and .version!=$x.version) | "\(.id) \($x.version)->\(.version)"] | join(", ")')
expect "object versions unchanged (servers, If-Match)" "" "$vdiff"
lost_hist=$(comm -23 <(sort "$WORK/snap/before/history") <(sort "$WORK/snap/after/history") | wc -l)
expect "history: every old entry with author and role" "0 missing of $B_HIST" "$lost_hist missing of $B_HIST"
lost_audit=$(comm -23 <(sort "$WORK/snap/before/audit") <(sort "$WORK/snap/after/audit") | wc -l)
expect "audit: every old entry" "0 missing of $B_AUDIT" "$lost_audit missing of $B_AUDIT"
prof=$(jq -c --arg a "$SA" --arg b "$SB" --arg c "$SC" --arg d "$SD" \
  '[.[] | select(.id==$a or .id==$b or .id==$c or .id==$d) | {name, n:(.profiles|length)}] | sort_by(.name) | map("\(.name)=\(.n)") | join(" ")' \
  "$WORK/snap/after/servers")
expect "profile lists after migration" '"srv-a=1 srv-b=1 srv-c=0 srv-dead=1"' "$prof"

# Behaviour on the new panel with the old agent.
expect "command admin srv-a" "200 ok" "$(cmd admin "$SA" list)"
expect "command moderator srv-a (list of his group)" "200 ok" "$(cmd mod "$SA" list)"
expect "command supporter srv-a (passable)" "200 ok" "$(cmd sup "$SA" list)"
r=$(cmd sup "$SA" 'whitelist add eve')
expect "command supporter not passable is refused" "denied" "$([ "${r%% *}" = 200 ] && echo "$r" || echo denied)"
call admin GET "/servers/$SA/templates"
expect "command templates of srv-a (new feature)" "200 0" "$CODE $(out | jq '.items | length' 2>/dev/null)"
agent_up . || true
OLD_AGENT_VERSION=$(docker image inspect "$OLD_AGENT" --format '{{index .Config.Labels "org.opencontainers.image.version"}}')
expect "old agent connected to the new panel" "true $OLD_AGENT_VERSION" \
  "$(out | jq -r --arg id "$AGENT_ID" '.items[] | select(.id==$id) | "\(.connected) \(.version)"')"
call admin GET "/servers/$SA"
expect "container state of srv-a via the old agent" "running true" "$(out | jq -r '"\(.runtime.container.state) \(.runtime.container.agent_connected)"')"
caps=$(out | jq -r '.runtime.container.capabilities // [] | join(",")')
nv=$(out | jq -r '[.runtime.game_version // empty, (.runtime.container.volumes // [] | .[] | .size_bytes // .size // empty)] | length')
expect "features the old agent lacks are absent (game version, volume size)" "0" "$nv"
log "capabilities of the old agent: ${caps:-none}"
call admin GET "/servers/$SA/logs"
expect "container logs through the old agent" 200 "$CODE"

panel_errors() { grep -cE 'level=error|panic|fatal error' "$1"; }
dc logs --no-color panel >"$PLOG" 2>&1
dc logs --no-color agent >"$WORK/out/agent-old.log" 2>&1
expect "new panel log: no error or panic" 0 "$(panel_errors "$PLOG")"
expect "old agent log: no error" 0 "$(panel_errors "$WORK/out/agent-old.log")"
grep -E 'level=(error|warn)' "$PLOG" | cut -c1-240 | head -n 10 | while IFS= read -r l; do log "panel: $l"; done

# --- 5. new agent -----------------------------------------------------------

write_env "$NEW_PANEL" "$NEW_AGENT"
log "updating the agent"
dc up -d --no-deps agent >/dev/null 2>&1 || die "new agent up"
NEW_AGENT_VERSION=$(docker image inspect "$NEW_AGENT" --format '{{index .Config.Labels "org.opencontainers.image.version"}}')
agent_up "^${NEW_AGENT_VERSION//./\\.}$" && ok=0 || ok=1
check "new agent reconnects with its pairing" "connected, version $NEW_AGENT_VERSION" \
  "$(out | jq -r --arg id "$AGENT_ID" '.items[] | select(.id==$id) | "connected=\(.connected), version \(.version)"')" $ok
call admin GET /agents
expect "still one agent" 1 "$(out | jq '.items | length')"
dc logs --no-color agent >"$WORK/out/agent-new.log" 2>&1
dc logs --no-color panel >"$PLOG" 2>&1
expect "new agent log: no error" 0 "$(panel_errors "$WORK/out/agent-new.log")"
expect "panel log after agent update: no error or panic" 0 "$(panel_errors "$PLOG")"
expect "command admin srv-a with new agent" "200 ok" "$(cmd admin "$SA" list)"

# --- 6. database after ------------------------------------------------------

dc stop panel >/dev/null 2>&1
dbstat counts /data/rconductor.db >"$WORK/snap/after/tables" || die "dbstat counts after"
expect "schema version in goose_db_version" "$EXPECT_SCHEMA" "$(dbstat query /data/rconductor.db 'SELECT MAX(version_id) FROM goose_db_version')"
expect "server_profiles rows" "3" "$(dbstat query /data/rconductor.db 'SELECT COUNT(*) FROM server_profiles')"
shrunk=$(join -t "$(printf '\t')" <(sort "$WORK/snap/before/tables") <(sort "$WORK/snap/after/tables") |
  awk -F '\t' '$3 < $2 && $1 != "sessions" {printf "%s %s->%s ", $1, $2, $3}')
expect "no table lost rows (sessions aside)" "" "$shrunk"
gone=$(comm -23 <(cut -f1 "$WORK/snap/before/tables" | sort) <(cut -f1 "$WORK/snap/after/tables" | sort) | tr '\n' ' ')
expect "no table dropped" "" "$gone"
log "tables before/after:"
join -a 1 -a 2 -e - -o 0,1.2,2.2 -t "$(printf '\t')" <(sort "$WORK/snap/before/tables") <(sort "$WORK/snap/after/tables") |
  awk -F '\t' '{printf "  %-28s %6s %6s\n", $1, $2, $3}'

summary
! grep -q '^FAIL' "$RESULTS"
