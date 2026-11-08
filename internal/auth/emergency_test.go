// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"bytes"
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/auth/password"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logtest"
	"github.com/NightDragon1/rconductor/internal/store"
)

var fastParams = password.Params{Time: 1, MemoryKiB: 64, Threads: 1, SaltLen: 16, KeyLen: 32}

type fixture struct {
	st     *store.Store
	hasher *password.Hasher
	logs   *bytes.Buffer
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	logs := &bytes.Buffer{}
	log := logging.New(logs, "panel", config.Log{Format: "json"}).Module("auth")
	if _, err := st.Migrate(ctx, log, store.MigrateOptions{}); err != nil {
		t.Fatal(err)
	}
	h, err := password.NewHasher(fastParams, 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{st: st, hasher: h, logs: logs}
}

func (f fixture) log() *logging.Factory {
	return logging.New(f.logs, "panel", config.Log{Format: "json"})
}

var printedPassword = regexp.MustCompile(`password: (\S+)`)

// TestEmergencyAdminOnce covers AUTH-003 and AUTH-007: the first start
// creates the admin and prints a random password once; the next start
// prints nothing.
func TestEmergencyAdminOnce(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	var console bytes.Buffer
	if err := EnsureEmergencyAdmin(ctx, f.st, f.hasher, EmergencyOptions{Enabled: true}, f.log().Module("auth"), &console); err != nil {
		t.Fatal(err)
	}
	m := printedPassword.FindStringSubmatch(console.String())
	if m == nil || len(m[1]) != initialPasswordLen {
		t.Fatalf("no initial password printed:\n%s", console.String())
	}
	pw := m[1]
	if n := logtest.WaitCount(t, f.logs, `"event":"auth.initial_password.issued"`, 1); n != 1 {
		t.Errorf("auth.initial_password.issued %d times, want 1:\n%s", n, f.logs.String())
	}
	if strings.Contains(f.logs.String(), pw) {
		t.Error("initial password appears in the event log")
	}

	var role, hash string
	var emergency int
	if err := f.st.DB.QueryRowContext(ctx,
		`SELECT role, password_hash, is_emergency FROM users WHERE pseudonym = ?`, EmergencyPseudonym,
	).Scan(&role, &hash, &emergency); err != nil {
		t.Fatal(err)
	}
	if role != RoleRCONAdmin || emergency != 1 || !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("admin = %s %d %q", role, emergency, hash)
	}
	if ok, err := f.hasher.Verify(ctx, hash, pw); !ok || err != nil {
		t.Errorf("printed password does not match the stored hash: %v %v", ok, err)
	}
	var audited int
	if err := f.st.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM audit_log WHERE action = 'emergency_admin.create' AND result = 'ok'`).Scan(&audited); err != nil || audited != 1 {
		t.Errorf("audit entries = %d, %v", audited, err)
	}

	// Second start: nothing new.
	console.Reset()
	if err := EnsureEmergencyAdmin(ctx, f.st, f.hasher, EmergencyOptions{Enabled: true}, f.log().Module("auth"), &console); err != nil {
		t.Fatal(err)
	}
	if n := logtest.WaitCount(t, f.logs, "auth.initial_password.issued", 1); console.Len() != 0 || n != 1 {
		t.Errorf("second start issued a password again:\n%s", console.String())
	}
}

func TestEmergencyAdminDisabled(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	var console bytes.Buffer
	if err := EnsureEmergencyAdmin(ctx, f.st, f.hasher, EmergencyOptions{}, f.log().Module("auth"), &console); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.st.DB.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil || n != 0 {
		t.Errorf("users = %d, %v", n, err)
	}
	if console.Len() != 0 {
		t.Errorf("password printed with the flag off:\n%s", console.String())
	}
}

func TestRandomPassword(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		pw, err := randomPassword(initialPasswordLen)
		if err != nil {
			t.Fatal(err)
		}
		if len(pw) != initialPasswordLen || strings.Trim(pw, alphabet) != "" || seen[pw] {
			t.Fatalf("bad password %q", pw)
		}
		if err := password.Validate(pw); err != nil {
			t.Fatalf("initial password breaks the length rule: %v", err)
		}
		seen[pw] = true
	}
}

// TestEmergencyAdminReset covers AUTH-011: a new initial password once,
// the old one and all sessions invalid, forced change, lock lifted, audit
// without password and auth.emergency_admin.reset.
func TestEmergencyAdminReset(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	var console bytes.Buffer
	if err := EnsureEmergencyAdmin(ctx, f.st, f.hasher, EmergencyOptions{Enabled: true}, f.log().Module("auth"), &console); err != nil {
		t.Fatal(err)
	}
	oldPw := printedPassword.FindStringSubmatch(console.String())[1]
	var id string
	if err := f.st.DB.QueryRowContext(ctx, `SELECT id FROM users WHERE is_emergency = 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	// A second factor the admin lost: the reset clears it too (AUTH-008).
	if _, err := f.st.DB.ExecContext(ctx, `UPDATE users SET must_change_password = 0, disabled_at = 't', totp_secret = x'01', totp_last_step = 9 WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.DB.ExecContext(ctx, `INSERT INTO sessions VALUES ('aaaaaaaa11', ?, 't', 't', 't', 'ip', 'ua')`, id); err != nil {
		t.Fatal(err)
	}

	console.Reset()
	if err := EnsureEmergencyAdmin(ctx, f.st, f.hasher, EmergencyOptions{Enabled: true, Reset: true}, f.log().Module("auth"), &console); err != nil {
		t.Fatal(err)
	}
	m := printedPassword.FindAllStringSubmatch(console.String(), -1)
	if len(m) != 1 || m[0][1] == oldPw {
		t.Fatalf("new initial password printed %d times:\n%s", len(m), console.String())
	}
	newPw := m[0][1]
	var hash string
	var must int
	var disabled any
	if err := f.st.DB.QueryRowContext(ctx, `SELECT password_hash, must_change_password, disabled_at FROM users WHERE id = ?`, id).Scan(&hash, &must, &disabled); err != nil {
		t.Fatal(err)
	}
	if ok, _ := f.hasher.Verify(ctx, hash, oldPw); ok {
		t.Error("old password still valid")
	}
	if ok, _ := f.hasher.Verify(ctx, hash, newPw); !ok || must != 1 || disabled != nil {
		t.Errorf("after reset: verify=%v must=%d disabled=%v", ok, must, disabled)
	}
	var totpLeft int
	_ = f.st.DB.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE id = ? AND (totp_secret IS NOT NULL OR totp_last_step != 0)`, id).Scan(&totpLeft)
	if totpLeft != 0 {
		t.Error("second factor survives the reset")
	}
	var sessions, audited int
	_ = f.st.DB.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE user_id = ?`, id).Scan(&sessions)
	_ = f.st.DB.QueryRowContext(ctx, `SELECT count(*) FROM audit_log WHERE action = 'emergency_admin.reset' AND payload NOT LIKE ?`, "%"+newPw+"%").Scan(&audited)
	if sessions != 0 || audited != 1 {
		t.Errorf("sessions=%d audit=%d", sessions, audited)
	}
	logtest.Wait(t, f.logs, []string{`"event":"auth.emergency_admin.reset"`, `"event":"auth.session.revoked"`, "remove RCD_EMERGENCY_ADMIN_RESET"}...)
	logs := f.logs.String()
	if strings.Contains(logs, newPw) || strings.Contains(logs, oldPw) {
		t.Error("password in the event log")
	}

	// With the emergency admin disabled the reset does nothing.
	console.Reset()
	if err := EnsureEmergencyAdmin(ctx, f.st, f.hasher, EmergencyOptions{Reset: true}, f.log().Module("auth"), &console); err != nil || console.Len() != 0 {
		t.Errorf("reset with the flag off: %v %q", err, console.String())
	}
}
