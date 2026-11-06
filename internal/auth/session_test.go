// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/auth/password"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logtest"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

const testPassword = "a long enough password"

type sessionFixture struct {
	st   *store.Store
	svc  *Service
	logs *bytes.Buffer
	now  time.Time
	// delaysMu guards delays: parallel logins sleep at the same time.
	delaysMu sync.Mutex
	delays   []time.Duration
}

func newSessionFixture(t *testing.T, emergency bool) *sessionFixture {
	t.Helper()
	st := storetest.New(t)
	if err := EnsureDefaults(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	h, err := password.NewHasher(fastParams, 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	f := &sessionFixture{st: st, logs: logs, now: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
	f.svc = NewService(st, h, logging.New(logs, "panel", config.Log{Format: "json"}).Module("auth"), emergency)
	f.svc.now = func() time.Time { return f.now }
	f.svc.sleep = func(_ context.Context, d time.Duration) error {
		f.delaysMu.Lock()
		defer f.delaysMu.Unlock()
		f.delays = append(f.delays, d)
		return nil
	}
	return f
}

func (f *sessionFixture) user(t *testing.T, u storetest.User) string {
	t.Helper()
	hash, err := f.svc.hasher.Hash(context.Background(), testPassword)
	if err != nil {
		t.Fatal(err)
	}
	u.PasswordHash = hash
	return storetest.CreateUser(t, f.st, u)
}

var client = Client{IP: netip.MustParseAddr("192.0.2.10"), UserAgent: "test", RequestID: "req-1"}

func (f *sessionFixture) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := f.st.DB.QueryRowContext(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestLoginAndAuthenticate(t *testing.T) {
	ctx := context.Background()
	f := newSessionFixture(t, true)
	id := f.user(t, storetest.User{Pseudonym: "Mod", Role: RoleModerator})

	token, p, err := f.svc.Login(ctx, "mod", testPassword, client) // pseudonym ignores case
	if err != nil {
		t.Fatal(err)
	}
	if p.UserID != id || p.Role != RoleModerator || p.Pseudonym != "Mod" {
		t.Errorf("principal = %+v", p)
	}
	if !p.ExpiresAt.Equal(f.now.Add(DefaultIdleTimeout)) {
		t.Errorf("expires = %v", p.ExpiresAt)
	}
	// Only the hash is stored (Design 4.1).
	if f.count(t, `SELECT count(*) FROM sessions WHERE id_hash = ?`, token) != 0 ||
		f.count(t, `SELECT count(*) FROM sessions WHERE id_hash = ?`, HashToken(token)) != 1 {
		t.Error("session not stored as hash")
	}
	if f.count(t, `SELECT count(*) FROM audit_log WHERE action = 'login' AND result = 'ok' AND actor_id = ?`, id) != 1 {
		t.Error("login not audited")
	}
	logtest.Wait(t, f.logs, `"event":"auth.login.ok"`, `"session":"`+p.SessionRef()+`"`)
	if strings.Contains(f.logs.String(), token) || strings.Contains(f.logs.String(), testPassword) {
		t.Error("token or password in the log")
	}

	got, err := f.svc.Authenticate(ctx, token)
	if err != nil || got.UserID != id || got.SessionHash != p.SessionHash {
		t.Fatalf("Authenticate = %+v, %v", got, err)
	}
	if _, err := f.svc.Authenticate(ctx, token+"x"); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("wrong token: %v", err)
	}

	if err := f.svc.Logout(ctx, got, "req-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("after logout: %v", err)
	}
	logtest.Wait(t, f.logs, `"event":"auth.logout"`)
}

func TestLoginFailuresLookAlike(t *testing.T) {
	ctx := context.Background()
	f := newSessionFixture(t, false)
	f.user(t, storetest.User{Pseudonym: "Mod"})
	f.user(t, storetest.User{Pseudonym: "Gone", Disabled: true})
	f.user(t, storetest.User{Pseudonym: "admin", Role: RoleRCONAdmin, Emergency: true})

	for _, tc := range []struct{ name, pseudonym, pw string }{
		{"wrong password", "Mod", "wrong password!!"},
		{"unknown user", "nobody", testPassword},
		{"disabled user", "Gone", testPassword},
		{"emergency admin with flag off", "admin", testPassword},
	} {
		if _, _, err := f.svc.Login(ctx, tc.pseudonym, tc.pw, client); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("%s: %v, want ErrInvalidCredentials", tc.name, err)
		}
	}
	if n := f.count(t, `SELECT count(*) FROM audit_log WHERE action = 'login' AND result = 'denied'`); n != 4 {
		t.Errorf("denied logins audited = %d, want 4", n)
	}
	if n := logtest.WaitCount(t, f.logs, `"event":"auth.login.failed"`, 4); n != 4 {
		t.Errorf("auth.login.failed = %d, want 4", n)
	}
	// The pseudonym of an unknown user is never recorded.
	if strings.Contains(f.logs.String(), "nobody") || f.count(t, `SELECT count(*) FROM audit_log WHERE payload LIKE '%nobody%'`) != 0 {
		t.Error("unknown pseudonym recorded")
	}
	if f.count(t, `SELECT count(*) FROM sessions`) != 0 {
		t.Error("failed login created a session")
	}
}

func TestSessionExpiry(t *testing.T) {
	ctx := context.Background()
	f := newSessionFixture(t, true)
	f.user(t, storetest.User{Pseudonym: "Mod"})
	token, _, err := f.svc.Login(ctx, "Mod", testPassword, client)
	if err != nil {
		t.Fatal(err)
	}

	// Activity keeps the session alive up to the maximum lifetime.
	start := f.now
	for f.now.Sub(start) < DefaultMaxLifetime-DefaultIdleTimeout {
		f.now = f.now.Add(DefaultIdleTimeout - time.Hour)
		if _, err := f.svc.Authenticate(ctx, token); err != nil {
			t.Fatalf("active session ended after %v: %v", f.now.Sub(start), err)
		}
	}
	f.now = start.Add(DefaultMaxLifetime)
	if _, err := f.svc.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("session beyond maximum lifetime: %v", err)
	}
	if f.count(t, `SELECT count(*) FROM sessions`) != 0 {
		t.Error("expired session not removed")
	}

	// Idle timeout.
	token, _, err = f.svc.Login(ctx, "Mod", testPassword, client)
	if err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(DefaultIdleTimeout + time.Second)
	if _, err := f.svc.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("idle session: %v", err)
	}
}

func TestSessionEndsWhenUserDisabled(t *testing.T) {
	ctx := context.Background()
	f := newSessionFixture(t, true)
	id := f.user(t, storetest.User{Pseudonym: "Mod"})
	token, _, err := f.svc.Login(ctx, "Mod", testPassword, client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.DB.ExecContext(ctx, `UPDATE users SET disabled_at = ? WHERE id = ?`, store.Now(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("session of disabled user: %v", err)
	}
}

func TestSessionSettings(t *testing.T) {
	ctx := context.Background()
	f := newSessionFixture(t, true)
	if _, err := f.st.DB.ExecContext(ctx, `UPDATE settings SET value = '"30m"' WHERE key = ?`, SettingIdleTimeout); err != nil {
		t.Fatal(err)
	}
	f.user(t, storetest.User{Pseudonym: "Mod"})
	token, p, err := f.svc.Login(ctx, "Mod", testPassword, client)
	if err != nil {
		t.Fatal(err)
	}
	if !p.ExpiresAt.Equal(f.now.Add(30 * time.Minute)) {
		t.Errorf("expires = %v, want 30m idle", p.ExpiresAt)
	}
	f.now = f.now.Add(31 * time.Minute)
	if _, err := f.svc.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("configured idle timeout ignored: %v", err)
	}
}
