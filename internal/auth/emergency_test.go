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
	if err := EnsureEmergencyAdmin(ctx, f.st, f.hasher, true, f.log().Module("auth"), &console); err != nil {
		t.Fatal(err)
	}
	m := printedPassword.FindStringSubmatch(console.String())
	if m == nil || len(m[1]) != initialPasswordLen {
		t.Fatalf("no initial password printed:\n%s", console.String())
	}
	pw := m[1]
	if strings.Contains(f.logs.String(), pw) {
		t.Error("initial password appears in the event log")
	}
	if n := strings.Count(f.logs.String(), `"event":"auth.initial_password.issued"`); n != 1 {
		t.Errorf("auth.initial_password.issued %d times, want 1:\n%s", n, f.logs.String())
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
	if err := EnsureEmergencyAdmin(ctx, f.st, f.hasher, true, f.log().Module("auth"), &console); err != nil {
		t.Fatal(err)
	}
	if console.Len() != 0 || strings.Count(f.logs.String(), "auth.initial_password.issued") != 1 {
		t.Errorf("second start issued a password again:\n%s", console.String())
	}
}

func TestEmergencyAdminDisabled(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	var console bytes.Buffer
	if err := EnsureEmergencyAdmin(ctx, f.st, f.hasher, false, f.log().Module("auth"), &console); err != nil {
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
