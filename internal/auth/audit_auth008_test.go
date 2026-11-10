// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base32"
	"encoding/json"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// auditRow is one row of audit_log; NULL columns read as "".
type auditRow struct {
	Kind, ActorID, ActorRole, SessionID, TargetType, TargetID, Action, Payload, Result, Reason string
}

// auditRows returns the rows with the action, oldest first.
func auditRows(t *testing.T, db *sql.DB, action string) []auditRow {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT kind, coalesce(actor_id, ''), coalesce(actor_role, ''),
		coalesce(session_id, ''), coalesce(target_type, ''), coalesce(target_id, ''), action, payload, result,
		coalesce(reason_code, '') FROM audit_log WHERE action = ? ORDER BY ts, id`, action)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []auditRow
	for rows.Next() {
		var r auditRow
		if err := rows.Scan(&r.Kind, &r.ActorID, &r.ActorRole, &r.SessionID, &r.TargetType, &r.TargetID, &r.Action,
			&r.Payload, &r.Result, &r.Reason); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// auditDump is the whole audit log as text, for the check that no secret
// is in it.
func auditDump(t *testing.T, db *sql.DB) string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT id, ts, kind, coalesce(actor_id, ''), coalesce(actor_role, ''),
		coalesce(session_id, ''), coalesce(server_id, ''), coalesce(target_type, ''), coalesce(target_id, ''), action,
		payload, result, coalesce(reason_code, '') FROM audit_log`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var b strings.Builder
	cols := make([]string, 13)
	ptrs := make([]any, len(cols))
	for i := range cols {
		ptrs[i] = &cols[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		b.WriteString(strings.Join(cols, "|"))
		b.WriteByte('\n')
	}
	return b.String()
}

func payloadOf(t *testing.T, raw string) map[string]any {
	t.Helper()
	m := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("payload %q: %v", raw, err)
	}
	return m
}

func auditCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM audit_log`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestAUTH008AuditEnable: switching the own second factor on is audited
// with the user as actor and target, without the secret.
func TestAUTH008AuditEnable(t *testing.T) {
	f := newSFFixture(t)
	id := f.user(t, storetest.User{Pseudonym: "kiwi"})
	key := f.enable(t, "kiwi")
	got := auditRows(t, f.st.DB, "totp.enable")
	if len(got) != 1 {
		t.Fatalf("totp.enable entries = %+v", got)
	}
	e := got[0]
	if e.Kind != "auth" || e.ActorID != id || e.ActorRole != "moderator" || e.SessionID == "" ||
		e.TargetType != "user" || e.TargetID != id || e.Result != "ok" {
		t.Errorf("entry = %+v", e)
	}
	if strings.Contains(auditDump(t, f.st.DB), base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key)) {
		t.Error("TOTP secret in the audit log")
	}
}

// TestAUTH008AuditRecoveryRequest: a recovery request for a user with an
// address and the second factor on writes one entry without actor, with
// the request ID and only the domain of the address; neither link nor
// address is in the audit log. Requests for an unknown name, a user
// without address and a user without second factor write nothing, so the
// audit log holds only what a real recovery mail stands for.
func TestAUTH008AuditRecoveryRequest(t *testing.T) {
	f := newSFFixture(t)
	ctx := context.Background()
	id := f.user(t, storetest.User{Pseudonym: "kiwi"})
	f.user(t, storetest.User{Pseudonym: "plain"})
	f.user(t, storetest.User{Pseudonym: "noaddr"})
	for _, name := range []string{"kiwi", "plain"} {
		p, _, err := f.login(t, name, client)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.svc.SetEmail(ctx, p, client, name+"@example.org", testPassword, ""); err != nil {
			t.Fatal(err)
		}
	}
	f.enable(t, "kiwi")
	f.enable(t, "noaddr")

	before := auditCount(t, f.st.DB)
	// Each request from its own address, so that the limit per client
	// stays out of the way.
	ip := client.IP
	for _, name := range []string{"nobody", "plain", "noaddr"} {
		ip = ip.Next()
		f.svc.RequestRecovery(ctx, name, Client{IP: ip, RequestID: "req-" + name})
	}
	f.svc.WaitMail()
	if n := auditCount(t, f.st.DB); n != before {
		t.Errorf("requests without a mail wrote %d entries", n-before)
	}

	f.svc.RequestRecovery(ctx, "kiwi", Client{IP: ip.Next(), RequestID: "req-rec"})
	f.svc.WaitMail()
	got := auditRows(t, f.st.DB, "totp.recovery_request")
	if len(got) != 1 {
		t.Fatalf("totp.recovery_request entries = %+v", got)
	}
	e := got[0]
	if e.Kind != "auth" || e.ActorID != "" || e.ActorRole != "" || e.SessionID != "" ||
		e.TargetType != "user" || e.TargetID != id || e.Result != "ok" || e.Reason != "" {
		t.Errorf("entry = %+v", e)
	}
	p := payloadOf(t, e.Payload)
	if len(p) != 2 || p["request_id"] != "req-rec" || p["domain"] != "example.org" {
		t.Errorf("payload = %s", e.Payload)
	}
	mails := f.mail.all()
	if len(mails) != 1 {
		t.Fatalf("mails = %d", len(mails))
	}
	token := strings.TrimPrefix(mails[0].link, "https://panel.example.org/recover#")
	dump := auditDump(t, f.st.DB)
	for _, secretText := range []string{token, HashToken(token), "kiwi@", "plain@"} {
		if strings.Contains(dump, secretText) {
			t.Errorf("%q in the audit log", secretText)
		}
	}
}

// TestAUTH008AuditRecoveryRedeem: redeeming the link writes totp.disable
// with cause recovery, the user as actor and target; neither link nor
// password is in the audit log. A refused link writes no totp.disable.
func TestAUTH008AuditRecoveryRedeem(t *testing.T) {
	f := newSFFixture(t)
	ctx := context.Background()
	id := f.user(t, storetest.User{Pseudonym: "kiwi"})
	p, _, _ := f.login(t, "kiwi", client)
	if err := f.svc.SetEmail(ctx, p, client, "kiwi@example.org", testPassword, ""); err != nil {
		t.Fatal(err)
	}
	f.enable(t, "kiwi")
	f.svc.RequestRecovery(ctx, "kiwi", client)
	f.svc.WaitMail()
	mails := f.mail.all()
	if len(mails) != 1 {
		t.Fatalf("mails = %d", len(mails))
	}
	token := strings.TrimPrefix(mails[0].link, "https://panel.example.org/recover#")

	if err := f.svc.RedeemRecovery(ctx, token+"x", testPassword, client); err == nil {
		t.Fatal("wrong link accepted")
	}
	if got := auditRows(t, f.st.DB, "totp.disable"); len(got) != 0 {
		t.Errorf("refused link audited as disable: %+v", got)
	}
	if err := f.svc.RedeemRecovery(ctx, token, testPassword, client); err != nil {
		t.Fatal(err)
	}
	got := auditRows(t, f.st.DB, "totp.disable")
	if len(got) != 1 {
		t.Fatalf("totp.disable entries = %+v", got)
	}
	e := got[0]
	if e.Kind != "auth" || e.ActorID != id || e.ActorRole != "moderator" || e.SessionID != "" ||
		e.TargetType != "user" || e.TargetID != id || e.Result != "ok" {
		t.Errorf("entry = %+v", e)
	}
	if pl := payloadOf(t, e.Payload); len(pl) != 1 || pl["cause"] != "recovery" {
		t.Errorf("payload = %s", e.Payload)
	}
	dump := auditDump(t, f.st.DB)
	for _, secretText := range []string{token, HashToken(token), testPassword, "kiwi@"} {
		if strings.Contains(dump, secretText) {
			t.Errorf("%q in the audit log", secretText)
		}
	}
}

// TestAUTH008AuditEmergencyReset: the reset after AUTH-011 records in its
// entry that the second factor was cleared, without actor and without the
// new password.
func TestAUTH008AuditEmergencyReset(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	var console bytes.Buffer
	if err := EnsureEmergencyAdmin(ctx, f.st, f.hasher, EmergencyOptions{Enabled: true}, f.log().Module("auth"), &console); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := f.st.DB.QueryRowContext(ctx, `SELECT id FROM users WHERE is_emergency = 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	reset := func() string {
		t.Helper()
		console.Reset()
		if err := EnsureEmergencyAdmin(ctx, f.st, f.hasher, EmergencyOptions{Enabled: true, Reset: true}, f.log().Module("auth"), &console); err != nil {
			t.Fatal(err)
		}
		return printedPassword.FindStringSubmatch(console.String())[1]
	}
	if _, err := f.st.DB.ExecContext(ctx, `UPDATE users SET totp_secret = x'01', totp_last_step = 9 WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	pw1 := reset()
	pw2 := reset() // without a second factor
	got := auditRows(t, f.st.DB, "emergency_admin.reset")
	if len(got) != 2 {
		t.Fatalf("emergency_admin.reset entries = %+v", got)
	}
	for i, want := range []float64{1, 0} {
		e := got[i]
		if e.Kind != "auth" || e.ActorID != "" || e.TargetType != "user" || e.TargetID != id || e.Result != "ok" {
			t.Errorf("entry %d = %+v", i, e)
		}
		if p := payloadOf(t, e.Payload); p["totp_cleared"] != want {
			t.Errorf("entry %d payload = %s, want totp_cleared %v", i, e.Payload, want)
		}
	}
	dump := auditDump(t, f.st.DB)
	if strings.Contains(dump, pw1) || strings.Contains(dump, pw2) {
		t.Error("initial password in the audit log")
	}
}
