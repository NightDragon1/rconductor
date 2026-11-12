// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/NightDragon1/rconductor/internal/auth"
)

type auditEntry struct {
	Kind, ActorID, ActorRole, TargetType, TargetID, Payload, Result, Reason string
}

func (f *fixture) auditOf(action string) []auditEntry {
	f.t.Helper()
	rows, err := f.st.DB.QueryContext(context.Background(), `SELECT kind, coalesce(actor_id, ''), coalesce(actor_role, ''),
		coalesce(target_type, ''), coalesce(target_id, ''), payload, result, coalesce(reason_code, '')
		FROM audit_log WHERE action = ? ORDER BY ts, id`, action)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []auditEntry
	for rows.Next() {
		var e auditEntry
		if err := rows.Scan(&e.Kind, &e.ActorID, &e.ActorRole, &e.TargetType, &e.TargetID, &e.Payload, &e.Result, &e.Reason); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		f.t.Fatal(err)
	}
	return out
}

// TestAUTH008AuditAdminReset: resetting the second factor of a managed user
// is audited with the manager as actor, the user as target and whether a
// second factor was on; the stored secret is not in the entry. A refused
// reset is audited as denied.
func TestAUTH008AuditAdminReset(t *testing.T) {
	f := newProfiles(t)
	ctx := context.Background()
	f.exec(`UPDATE users SET totp_secret = x'5345435245545345', totp_last_step = 7 WHERE id = ?`, f.ids["m"])
	if err := f.p.ResetSecondFactor(ctx, f.as("sa"), f.ids["m"]); err != nil {
		t.Fatal(err)
	}
	// Again, with nothing left to reset.
	if err := f.p.ResetSecondFactor(ctx, f.as("sa"), f.ids["m"]); err != nil {
		t.Fatal(err)
	}
	// A user the server admin does not manage.
	if err := f.p.ResetSecondFactor(ctx, f.as("sa"), f.ids["m2"]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reset of an unmanaged user: %v", err)
	}
	got := f.auditOf("totp.reset")
	if len(got) != 3 {
		t.Fatalf("totp.reset entries = %+v", got)
	}
	for i, want := range []string{`{"was_on":true}`, `{"was_on":false}`} {
		e := got[i]
		if e.Kind != "admin" || e.ActorID != f.ids["sa"] || e.ActorRole != "server_admin" || e.TargetType != "user" ||
			e.TargetID != f.ids["m"] || e.Result != "ok" || e.Payload != want {
			t.Errorf("entry %d = %+v", i, e)
		}
	}
	if d := got[2]; d.ActorID != f.ids["sa"] || d.TargetID != f.ids["m2"] || d.Result != "denied" || d.Reason != "not_found" {
		t.Errorf("denied entry = %+v", d)
	}
	if f.count(`SELECT count(*) FROM audit_log WHERE payload LIKE '%SECRETSE%' OR payload LIKE '%5345435245545345%'`) != 0 {
		t.Error("second-factor secret in the audit log")
	}
}

// TestAUTH008AuditTOTPRequired: requiring the second factor for everyone is
// a settings change audited with the RCON admin as actor and the old and
// new value of auth.totp_required; another role is refused and audited as
// denied with the key tried.
func TestAUTH008AuditTOTPRequired(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := auth.EnsureSecondFactorDefaults(ctx, f.st); err != nil {
		t.Fatal(err)
	}
	in := map[string]json.RawMessage{auth.SettingTOTPRequired: json.RawMessage(`true`)}
	if _, err := f.svc.UpdateSettings(ctx, f.as("sa"), in, f.version("settings", "")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("server admin: %v", err)
	}
	if _, err := f.svc.UpdateSettings(ctx, f.as("admin"), in, f.version("settings", "")); err != nil {
		t.Fatal(err)
	}
	var on bool
	if err := f.st.GlobalSetting(ctx, auth.SettingTOTPRequired, &on); err != nil || !on {
		t.Fatalf("stored %v, %v", on, err)
	}
	// The same value again changes nothing and writes nothing.
	if _, err := f.svc.UpdateSettings(ctx, f.as("admin"), in, f.version("settings", "")); err != nil {
		t.Fatal(err)
	}
	got := f.auditOf("settings.update")
	if len(got) != 2 {
		t.Fatalf("settings.update entries = %+v", got)
	}
	d := got[0]
	if d.Kind != "admin" || d.ActorID != f.ids["sa"] || d.Result != "denied" || d.Reason != "forbidden" ||
		d.TargetType != "settings" || d.Payload != `{"keys":["auth.totp_required"]}` {
		t.Errorf("denied entry = %+v", d)
	}
	e := got[1]
	if e.Kind != "admin" || e.ActorID != f.ids["admin"] || e.ActorRole != "rcon_admin" || e.TargetType != "settings" ||
		e.TargetID != "" || e.Result != "ok" {
		t.Errorf("entry = %+v", e)
	}
	var p map[string]map[string]json.RawMessage
	if err := json.Unmarshal([]byte(e.Payload), &p); err != nil {
		t.Fatal(err)
	}
	c, ok := p[auth.SettingTOTPRequired]
	if len(p) != 1 || !ok || string(c["from"]) != "false" || string(c["to"]) != "true" {
		t.Errorf("payload = %s", e.Payload)
	}
}
