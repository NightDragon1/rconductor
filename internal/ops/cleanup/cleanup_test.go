// SPDX-License-Identifier: AGPL-3.0-or-later

package cleanup

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logtest"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
)

func at(t time.Time) string { return t.UTC().Format(store.TimeLayout) }

// TestRun covers AUD-003 (entries beyond the retention are gone after the
// next run) and Design 4.4.
func TestRun(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	if err := users.EnsureAuditDefaults(ctx, st); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := st.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	count := func(q string) int {
		t.Helper()
		var n int
		if err := st.DB.QueryRowContext(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	audit := `INSERT INTO audit_log (id, ts, kind, actor_id, target_type, target_id, action, result) VALUES (?, ?, 'admin', ?, ?, ?, 'x', 'ok')`
	exec(audit, "old", at(now.AddDate(0, 0, -91)), nil, nil, nil)
	exec(audit, "recent", at(now.AddDate(0, 0, -89)), nil, nil, nil)

	admin := storetest.CreateUser(t, st, storetest.User{Pseudonym: "admin", Role: "rcon_admin"})
	gone := storetest.CreateUser(t, st, storetest.User{Pseudonym: "gone"})
	audited := storetest.CreateUser(t, st, storetest.User{Pseudonym: "audited"})
	referenced := storetest.CreateUser(t, st, storetest.User{Pseudonym: "referenced"})
	storetest.CreateUser(t, st, storetest.User{Pseudonym: "child", CreatedBy: referenced})
	exec(`UPDATE users SET deleted_at = 't' WHERE id IN (?, ?, ?)`, gone, audited, referenced)
	exec(audit, "ref", at(now), nil, "user", audited)

	exec(`INSERT INTO sessions VALUES ('expired', ?, 't', ?, ?, 'ip', 'ua')`, admin, at(now), at(now.Add(-time.Minute)))
	exec(`INSERT INTO sessions VALUES ('idle', ?, 't', ?, ?, 'ip', 'ua')`, admin, at(now.Add(-13*time.Hour)), at(now.Add(time.Hour)))
	exec(`INSERT INTO sessions VALUES ('live', ?, 't', ?, ?, 'ip', 'ua')`, admin, at(now), at(now.Add(time.Hour)))
	exec(`INSERT INTO invites (id, token_hash, created_by, role, created_at, expires_at) VALUES ('i1', 'h1', ?, 'moderator', 't', ?)`, admin, at(now.Add(-time.Hour)))
	exec(`INSERT INTO invites (id, token_hash, created_by, role, created_at, expires_at) VALUES ('i2', 'h2', ?, 'moderator', 't', ?)`, admin, at(now.Add(time.Hour)))

	var logs bytes.Buffer
	res, err := Run(ctx, st, logging.New(&logs, "panel", config.Log{Format: "json"}).Module("audit"), now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Audit != 1 || count(`SELECT count(*) FROM audit_log WHERE id = 'recent'`) != 1 {
		t.Errorf("audit purge = %+v", res)
	}
	logtest.Wait(t, &logs, `"event":"audit.purge"`, `"removed":1`)
	if res.Sessions != 2 || count(`SELECT count(*) FROM sessions`) != 1 {
		t.Errorf("sessions purge = %+v", res)
	}
	if res.Invites != 1 || count(`SELECT count(*) FROM invites`) != 1 {
		t.Errorf("invites purge = %+v", res)
	}
	if count(`SELECT count(*) FROM users WHERE pseudonym = 'gone'`) != 0 {
		t.Error("unreferenced deleted user kept")
	}
	if count(`SELECT count(*) FROM users WHERE pseudonym IN ('audited', 'referenced')`) != 2 {
		t.Error("referenced deleted user removed")
	}

	// A shorter retention set by the RCON admin applies on the next run.
	exec(`UPDATE settings SET value = '30' WHERE key = ?`, users.SettingAuditRetentionDays)
	if res, err := Run(ctx, st, logging.New(&logs, "panel", config.Log{}).Module("audit"), now); err != nil || res.Audit != 1 {
		t.Errorf("run with 30 days = %+v, %v", res, err)
	}
}
