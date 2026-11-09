// SPDX-License-Identifier: AGPL-3.0-or-later

package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/secret"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
)

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type fixture struct {
	st    *store.Store
	svc   *Service
	logs  *lockedBuf
	admin users.Request
	sa    users.Request
	cert  tls.Certificate
	roots *x509.CertPool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st := storetest.New(t)
	ctx := context.Background()
	if err := EnsureDefaults(ctx, st); err != nil {
		t.Fatal(err)
	}
	master := make([]byte, 32)
	_, _ = rand.Read(master)
	box, err := secret.New(master)
	if err != nil {
		t.Fatal(err)
	}
	logs := &lockedBuf{}
	log := logging.New(logs, "panel", config.Log{Format: "json"}).Module("mail")
	cert, roots := certFor(t)
	f := &fixture{st: st, logs: logs, cert: cert, roots: roots,
		svc: NewTestService(st, box, log, roots, 2*time.Second)}
	f.admin = users.Request{Actor: perm.Actor{ID: storetest.CreateUser(t, st, storetest.User{Pseudonym: "boss", Role: "rcon_admin"}), Role: perm.RCONAdmin}, Pseudonym: "boss", RequestID: "req-1"}
	f.sa = users.Request{Actor: perm.Actor{ID: storetest.CreateUser(t, st, storetest.User{Pseudonym: "sa", Role: "server_admin"}), Role: perm.ServerAdmin}, Pseudonym: "sa"}
	return f
}

func ptr[T any](v T) *T { return &v }

// set saves host, port, encryption and the credentials of the fake.
func (f *fixture) set(t *testing.T, srv *fakeSMTP, encryption string, auth bool) View {
	t.Helper()
	ctx := context.Background()
	cur, err := f.svc.Get(ctx, f.admin)
	if err != nil {
		t.Fatal(err)
	}
	in := Input{Host: ptr("localhost"), Port: ptr(srv.port()), Encryption: ptr(encryption), From: ptr("panel@example.org"),
		Username: ptr(""), Password: ptr(logging.Secret(""))}
	if auth {
		in.Username, in.Password = ptr(srv.user), ptr(logging.Secret(srv.password))
	}
	v, err := f.svc.Update(ctx, f.admin, in, &cur.Version)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (f *fixture) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := f.st.DB.QueryRowContext(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestSettings covers NFR-017 for the setup: only the RCON admin reads and
// changes it, with If-Match; the password is sealed, shown only as set and
// stays with a new host or port (D-050); user and password need STARTTLS
// or SSL; every change is audited without the password; bad input is
// refused.
func TestSettings(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	v, err := f.svc.Get(ctx, f.admin)
	if err != nil || v.Encryption != EncryptionSTARTTLS || v.Port != 587 || v.PasswordSet {
		t.Fatalf("default = %+v %v", v, err)
	}
	if _, err := f.svc.Get(ctx, f.sa); !errors.Is(err, users.ErrForbidden) {
		t.Errorf("server admin reads: %v", err)
	}
	if _, err := f.svc.Update(ctx, f.sa, Input{Host: ptr("x")}, &v.Version); !errors.Is(err, users.ErrForbidden) {
		t.Errorf("server admin writes: %v", err)
	}
	if _, err := f.svc.Update(ctx, f.admin, Input{Host: ptr("x")}, nil); !errors.Is(err, users.ErrPreconditionRequired) {
		t.Errorf("without If-Match: %v", err)
	}
	pw := logging.Secret("smtp-Secret-4711")
	v2, err := f.svc.Update(ctx, f.admin, Input{Host: ptr("mail.example.org"), Username: ptr("mailer"), Password: &pw, From: ptr("panel@example.org")}, &v.Version)
	if err != nil || !v2.PasswordSet || v2.Version != v.Version+1 {
		t.Fatalf("update = %+v %v", v2, err)
	}
	if _, err := f.svc.Update(ctx, f.admin, Input{Port: ptr(465)}, &v.Version); !errors.Is(err, users.ErrConflict) {
		t.Errorf("old version: %v", err)
	}
	var sealed []byte
	if err := f.st.DB.QueryRowContext(ctx, `SELECT password FROM smtp_settings`).Scan(&sealed); err != nil || bytes.Contains(sealed, []byte(pw)) {
		t.Errorf("password not sealed: %v", err)
	}
	// A new host and port keep the password (D-050).
	v3, err := f.svc.Update(ctx, f.admin, Input{Host: ptr("smtp.example.org"), Port: ptr(465), Encryption: ptr(EncryptionSSL)}, &v2.Version)
	if err != nil || !v3.PasswordSet || v3.Host != "smtp.example.org" {
		t.Errorf("host change = %+v %v", v3, err)
	}
	var inv *users.InvalidError
	for name, in := range map[string]Input{
		"user without TLS":    {Encryption: ptr(EncryptionNone)},
		"host with a space":   {Host: ptr("mail example.org")},
		"host with a port":    {Host: ptr("mail.example.org:25")},
		"host too long":       {Host: ptr(strings.Repeat("a", 254))},
		"port 0":              {Port: ptr(0)},
		"unknown encryption":  {Encryption: ptr("tls1.0")},
		"sender with CR LF":   {From: ptr("panel@example.org\r\nBcc: all@example.org")},
		"sender with a name":  {From: ptr("Panel <panel@example.org>")},
		"user with LF":        {Username: ptr("mailer\nX")},
		"password too long":   {Password: ptr(logging.Secret(strings.Repeat("p", 513)))},
		"sender too long":     {From: ptr(strings.Repeat("a", 250) + "@x.org")},
		"bidi in the sender":  {From: ptr("panel\u202e@example.org")},
		"control in the host": {Host: ptr("mail\x00.example.org")},
	} {
		if _, err := f.svc.Update(ctx, f.admin, in, &v3.Version); !errors.As(err, &inv) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Without user and password, none is fine.
	if _, err := f.svc.Update(ctx, f.admin, Input{Encryption: ptr(EncryptionNone), Username: ptr(""), Password: ptr(logging.Secret(""))}, &v3.Version); err != nil {
		t.Errorf("none without auth: %v", err)
	}
	if n := f.count(t, `SELECT count(*) FROM audit_log WHERE action = 'settings.smtp.update' AND result = 'ok'`); n != 3 {
		t.Errorf("audited changes = %d, want 3", n)
	}
	if n := f.count(t, `SELECT count(*) FROM audit_log WHERE action IN ('settings.smtp.read', 'settings.smtp.update') AND result = 'denied'`); n != 2 {
		t.Errorf("audited refusals = %d, want 2", n)
	}
	if n := f.count(t, `SELECT count(*) FROM audit_log WHERE payload LIKE ?`, "%"+string(pw)+"%"); n != 0 {
		t.Error("the password is in the audit")
	}
	if strings.Contains(f.logs.String(), string(pw)) {
		t.Error("the password is in the log")
	}
}

// TestDelivery covers NFR-017 for sending: STARTTLS and SSL with a checked
// certificate and authentication, none without it; the test mail is
// fixed; the log names only the domain (mail.sent).
func TestDelivery(t *testing.T) {
	for _, tc := range []struct {
		name       string
		encryption string
		auth       bool
		set        func(*fakeSMTP)
	}{
		{"starttls", EncryptionSTARTTLS, true, func(s *fakeSMTP) { s.starttls = true }},
		{"ssl", EncryptionSSL, true, func(s *fakeSMTP) { s.ssl = true }},
		{"none", EncryptionNone, false, func(*fakeSMTP) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			srv := startSMTP(t, f.cert, tc.set)
			f.set(t, srv, tc.encryption, tc.auth)
			res, err := f.svc.Test(context.Background(), f.admin, "someone@Example.COM")
			if err != nil || !res.OK {
				t.Fatalf("test = %+v %v", res, err)
			}
			cmds, msgs, plain := srv.seen()
			if len(msgs) != 1 || !strings.Contains(msgs[0], "Subject: "+testSubject) || !strings.Contains(msgs[0], testBody) {
				t.Errorf("messages = %q", msgs)
			}
			if tc.auth && !contains(cmds, "AUTH") {
				t.Error("no authentication")
			}
			if tc.encryption != EncryptionNone && (contains(plain, "AUTH") || contains(plain, "MAIL")) {
				t.Errorf("sent in plain text: %v", plain)
			}
			logs := f.logs.String()
			if !strings.Contains(logs, `"event":"mail.sent"`) || !strings.Contains(logs, `"domain":"example.com"`) || strings.Contains(logs, "someone") {
				t.Errorf("log = %s", logs)
			}
			if strings.Contains(logs, srv.password) {
				t.Error("the password is in the log")
			}
			if n := f.count(t, `SELECT count(*) FROM audit_log WHERE action = 'settings.smtp.test' AND result = 'ok'`); n != 1 {
				t.Errorf("audited tests = %d", n)
			}
		})
	}
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// TestFailureCategories covers NFR-017: a failure is named only by its
// category (mail.failed); STARTTLS is a must once chosen: a server
// that does not offer it fails as tls and gets nothing in plain text; an
// untrusted certificate fails as tls, with STARTTLS and SSL.
func TestFailureCategories(t *testing.T) {
	for _, tc := range []struct {
		name, encryption, want string
		auth                   bool
		set                    func(*fakeSMTP)
		untrusted              bool
	}{
		{"starttls not offered", EncryptionSTARTTLS, CategoryTLS, true, func(*fakeSMTP) {}, false},
		{"starttls untrusted", EncryptionSTARTTLS, CategoryTLS, true, func(s *fakeSMTP) { s.starttls = true }, true},
		{"ssl untrusted", EncryptionSSL, CategoryTLS, true, func(s *fakeSMTP) { s.ssl = true }, true},
		{"wrong password", EncryptionSTARTTLS, CategoryAuth, true, func(s *fakeSMTP) { s.starttls, s.password = true, "another" }, false},
		{"recipient refused", EncryptionNone, CategoryRejected, false, func(s *fakeSMTP) { s.rejectTo = true }, false},
		{"no answer", EncryptionNone, CategoryTimeout, false, func(s *fakeSMTP) { s.silent = true }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			srv := startSMTP(t, f.cert, tc.set)
			wantPW := srv.password
			srv.password = "smtp-Secret-4711" // what the panel sends
			f.set(t, srv, tc.encryption, tc.auth)
			srv.password = wantPW
			if tc.untrusted {
				f.svc.roots = x509.NewCertPool()
			}
			res, err := f.svc.Test(context.Background(), f.admin, "someone@example.com")
			if err != nil || res.OK || res.Category != tc.want {
				t.Fatalf("test = %+v %v, want %s", res, err, tc.want)
			}
			_, msgs, plain := srv.seen()
			if len(msgs) != 0 {
				t.Errorf("a message went out: %q", msgs)
			}
			if tc.encryption == EncryptionSTARTTLS && (contains(plain, "AUTH") || contains(plain, "MAIL")) {
				t.Errorf("fell back to plain text: %v", plain)
			}
			// The server's own reply text must not reach the log; its
			// code alone would also match a timestamp such as 08.550Z.
			logs := f.logs.String()
			if !strings.Contains(logs, `"event":"mail.failed"`) || !strings.Contains(logs, `"reason":"`+tc.want+`"`) ||
				strings.Contains(logs, "no such user") || strings.Contains(logs, "smtp-Secret-4711") {
				t.Errorf("log = %s", logs)
			}
			if n := f.count(t, `SELECT count(*) FROM audit_log WHERE action = 'settings.smtp.test' AND result = 'error' AND reason_code = ?`, tc.want); n != 1 {
				t.Errorf("audited failure = %d", n)
			}
		})
	}
	t.Run("closed port", func(t *testing.T) {
		f := newFixture(t)
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		port := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
		cur, _ := f.svc.Get(context.Background(), f.admin)
		if _, err := f.svc.Update(context.Background(), f.admin, Input{Host: ptr("127.0.0.1"), Port: ptr(port), Encryption: ptr(EncryptionNone), From: ptr("panel@example.org")}, &cur.Version); err != nil {
			t.Fatal(err)
		}
		if res, err := f.svc.Test(context.Background(), f.admin, "someone@example.com"); err != nil || res.Category != CategoryConnect {
			t.Errorf("closed port = %+v %v", res, err)
		}
	})
}

// TestTestMailLimits covers the abuse cases of the test mail: only the
// RCON admin, only a plain address, at most smtp.test_per_minute per user
// and minute, also when sent at once; a new minute counts anew.
func TestTestMailLimits(t *testing.T) {
	f := newFixture(t)
	srv := startSMTP(t, f.cert, func(s *fakeSMTP) { s.starttls = true })
	f.set(t, srv, EncryptionSTARTTLS, true)
	ctx := context.Background()
	if _, err := f.svc.Test(ctx, f.sa, "someone@example.com"); !errors.Is(err, users.ErrForbidden) {
		t.Errorf("server admin: %v", err)
	}
	var inv *users.InvalidError
	for _, bad := range []string{"", "no address", "a@b.org\r\nBcc: x@y.org", "Name <a@b.org>", strings.Repeat("a", 250) + "@b.org"} {
		if _, err := f.svc.Test(ctx, f.admin, bad); !errors.As(err, &inv) {
			t.Errorf("recipient %.30q: %v", bad, err)
		}
	}
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	f.svc.now = func() time.Time { return now }
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, limited := 0, 0
	for range 2 * DefaultTestPerMinute {
		wg.Go(func() {
			_, err := f.svc.Test(ctx, f.admin, "someone@example.com")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrRateLimited):
				limited++
			default:
				t.Errorf("test: %v", err)
			}
		})
	}
	wg.Wait()
	if ok != DefaultTestPerMinute || limited != DefaultTestPerMinute {
		t.Errorf("sent %d, limited %d, want %d each", ok, limited, DefaultTestPerMinute)
	}
	now = now.Add(time.Minute)
	if _, err := f.svc.Test(ctx, f.admin, "someone@example.com"); err != nil {
		t.Errorf("next minute: %v", err)
	}
}
