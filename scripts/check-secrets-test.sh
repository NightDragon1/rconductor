#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Checks scripts/check-secrets.sh (SEC-012, NFR-013) with one-line diffs:
# what must be found and the false alarms it must leave alone. The secret
# samples are put together at run time, so this file holds none.
set -euo pipefail
cd "$(dirname "$0")/.."
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fails=0
scan() { # file, added line
  printf -- '--- a/%s\n+++ b/%s\n@@ -0,0 +1 @@\n+%s\n' "$1" "$1" "$2" > "$tmp/diff"
  scripts/check-secrets.sh "$tmp/diff" >/dev/null 2>&1
}
found() { scan "$@" && { echo "not found: $1: $2" >&2; fails=$((fails + 1)); } || true; }
clean() { scan "$@" || { echo "false alarm: $1: $2" >&2; fails=$((fails + 1)); }; }

dash=-----
b36=$(printf 'a%.0s' $(seq 36))
b30=$(printf 'B%.0s' $(seq 30))
found deploy/key.pem "${dash}BEGIN PRIVATE KEY${dash}"
found docs/x.md "${dash}BEGIN RSA PRIVATE KEY${dash}"
found docs/x.md "${dash}BEGIN OPENSSH PRIVATE KEY${dash}"
found internal/x/x_test.go "k := \"${dash}BEGIN EC PRIVATE KEY${dash}\""
found scripts/x.sh "GH_TOKEN=gh""p_$b36"
found web/src/a.ts "const t = 'gh""s_$b36';"
found .env "X=github""_pat_$b30"
found internal/x/x_test.go "hub := \"dckr""_pat_$b30\""
found internal/config/c.go 'password := "Tr0ub4dor&3x"'
found deploy/compose/.env 'RCD_SMTP_PASSWORD=hunter2hunter2'
found config.yaml 'secret: s3cr3tvalue'
found internal/a/a.go 'Token = `abcdefgh1234`'
found docs/user/x.md 'smtp_password: "myrealpass"'

clean internal/x/x_test.go 'password := "correct horse battery"'
clean web/e2e/users.spec.ts "await page.fill('#password', 'Secret-Passw0rd!');"
clean scripts/x.sh 'password=$RCD_PASSWORD'
clean scripts/x.sh 'token="${GH_TOKEN}"'
clean internal/a/a.go 'token := os.Getenv("TOKEN")'
clean internal/a/a.go 'Password string `json:"password"`'
clean docs/user/x.md 'password: <your password>'
clean test/upgrade/run.sh "-e 's/(password: +)[^ ]+/\\1***/g'"
clean docs/user/x.md 'password: "***"'
clean docs/user/x.md 'token=short'
clean internal/a/a.go 'if password == "" {'
clean internal/a/a.go "x := \"gh""p_short\""
clean docs/x.md "BEGIN PUBLIC KEY"
clean config.yaml 'password: changeme-later # secret-scan: allow'
clean config.yaml 'password: example-password'
clean internal/auth/session.go 'return "", Principal{}, &SecondStepError{Token: pending, ExpiresAt: expires}'
clean internal/agenthub/throttle.go 'b = &bucket{tokens: float64(t.opts.Burst), last: now}'
clean deploy/compose/agent.yaml '      - agent-secrets:/secrets'
clean README.md "echo 'RCD_PAIRING_TOKEN=rcd1....' >> .env"
clean internal/store/queries/sqlite/agents.sql '-- name: UseAgentToken :execrows'
clean internal/store/queries/sqlite/totp.sql 'UPDATE users SET totp_secret = totp_pending, totp_last_step = ?'
clean web/src/pages/ChangePassword.tsx "autoComplete={name === 'current' ? 'current-password' : 'new-password'}"
clean web/src/i18n/en.json '"password": "Password",'
found config/panel.json '"smtp_password": "s3cr3tvalue",'

# Only added lines count.
printf -- '--- a/x.go\n+++ b/x.go\n@@ -1 +0,0 @@\n-password := "Tr0ub4dor&3x"\n' > "$tmp/diff"
scripts/check-secrets.sh "$tmp/diff" >/dev/null 2>&1 || { echo "a removed line counted" >&2; fails=$((fails + 1)); }

[ "$fails" -eq 0 ] || exit 1
echo "secret check ok"
