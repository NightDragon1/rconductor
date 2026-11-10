// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/auth/totp"
	"github.com/NightDragon1/rconductor/internal/store/secret"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

type sentMail struct{ to, link string }

type fakeMailer struct {
	mu   sync.Mutex
	sent []sentMail
}

func (m *fakeMailer) SendRecovery(_ context.Context, to, link string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, sentMail{to, link})
	return ""
}

func (m *fakeMailer) all() []sentMail {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sentMail(nil), m.sent...)
}

type sfFixture struct {
	*sessionFixture
	mail *fakeMailer
}

func newSFFixture(t *testing.T) *sfFixture {
	t.Helper()
	f := newSessionFixture(t, false)
	if err := EnsureSecondFactorDefaults(context.Background(), f.st); err != nil {
		t.Fatal(err)
	}
	master := make([]byte, 32)
	_, _ = rand.Read(master)
	box, err := secret.New(master)
	if err != nil {
		t.Fatal(err)
	}
	m := &fakeMailer{}
	f.svc.SetSecondFactor(SecondFactor{Box: box, Mail: m, LinkBase: "https://panel.example.org/"})
	return &sfFixture{sessionFixture: f, mail: m}
}

func (f *sfFixture) set(t *testing.T, key, value string) {
	t.Helper()
	if _, err := f.st.DB.ExecContext(context.Background(), `UPDATE settings SET value = ? WHERE key = ?`, value, key); err != nil {
		t.Fatal(err)
	}
}

// login signs in with the password; it returns the session or the token of
// the waiting second step.
func (f *sfFixture) login(t *testing.T, name string, c Client) (Principal, string, error) {
	t.Helper()
	_, p, err := f.svc.Login(context.Background(), name, testPassword, c)
	var step *SecondStepError
	if errors.As(err, &step) {
		return Principal{}, step.Token, err
	}
	return p, "", err
}

// enable sets up and confirms the second factor of name; it returns the
// raw secret.
func (f *sfFixture) enable(t *testing.T, name string) []byte {
	t.Helper()
	p, _, err := f.login(t, name, client)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	setup, err := f.svc.SetupTOTP(ctx, p, client, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setup.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(setup.URI, "otpauth://totp/RCONductor:"+name+"?") {
		t.Errorf("uri = %s", setup.URI)
	}
	if err := f.svc.ConfirmTOTP(ctx, p, client, totp.Code(key, totp.Step(f.now))); err != nil {
		t.Fatal(err)
	}
	return key
}

// code is the code of the next step, so that it was not used yet.
func (f *sfFixture) code(key []byte) string {
	f.now = f.now.Add(totp.Period)
	return totp.Code(key, totp.Step(f.now))
}

// TestSecondFactorLogin covers AUTH-008 and D-052: with the second factor
// on, the password opens only the second step; the right code starts the
// session and only then the session sent along ends; a code holds once;
// a wrong code is refused and counted.
func TestSecondFactorLogin(t *testing.T) {
	f := newSFFixture(t)
	f.user(t, storetest.User{Pseudonym: "kiwi"})
	key := f.enable(t, "kiwi")
	ctx := context.Background()

	old, _, err := f.login(t, "kiwi", client)
	if !errors.Is(err, ErrSecondFactorRequired) || old.SessionHash != "" {
		t.Fatalf("login with the second factor on: %+v %v", old, err)
	}
	sessionsBefore := f.count(t, `SELECT count(*) FROM sessions`)

	_, pending, _ := f.login(t, "kiwi", client)
	if f.count(t, `SELECT count(*) FROM sessions`) != sessionsBefore {
		t.Error("the password alone started a session")
	}
	if _, _, err := f.svc.LoginCode(ctx, pending, "000000", client); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong code: %v", err)
	}
	if !strings.Contains(f.logs.String(), `"cause":"wrong_code"`) {
		t.Error("wrong code not logged as auth.login.failed")
	}
	c := f.code(key)
	_, p, err := f.svc.LoginCode(ctx, pending, c, client)
	if err != nil || p.SessionHash == "" || p.TOTPSetupRequired {
		t.Fatalf("right code: %+v %v", p, err)
	}
	// The step holds once.
	if _, _, err := f.svc.LoginCode(ctx, pending, c, client); !errors.Is(err, ErrNoPending) {
		t.Errorf("second use of the step: %v", err)
	}
	// The same code a second time does not open another login (replay).
	_, pending2, _ := f.login(t, "kiwi", client)
	if _, _, err := f.svc.LoginCode(ctx, pending2, c, client); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("code used twice: %v", err)
	}
}

// TestSentAlongSessionEndsAfterTheCode covers D-052 with the second
// factor: the session sent along stays after the password and ends with
// the right code.
func TestSentAlongSessionEndsAfterTheCode(t *testing.T) {
	f := newSFFixture(t)
	f.user(t, storetest.User{Pseudonym: "kiwi"})
	ctx := context.Background()
	token, _, err := f.svc.Login(ctx, "kiwi", testPassword, client)
	if err != nil {
		t.Fatal(err)
	}
	key := f.enable(t, "kiwi")
	c := client
	c.SessionToken = token
	_, pending, _ := f.login(t, "kiwi", c)
	if _, err := f.svc.Authenticate(ctx, token); err != nil {
		t.Errorf("the session sent along ended after the password: %v", err)
	}
	if _, _, err := f.svc.LoginCode(ctx, pending, f.code(key), c); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("the session sent along stays after the code: %v", err)
	}
}

// TestSecondStepEnds covers the end of a waiting step (AUTH-008): after 5
// wrong codes, after auth.second_factor_window, after a change of the
// password, after a lock of the user and after a lockout.
func TestSecondStepEnds(t *testing.T) {
	ctx := context.Background()
	for name, end := range map[string]func(f *sfFixture, t *testing.T, pending string){
		"five wrong codes": func(f *sfFixture, t *testing.T, pending string) {
			for i := range MaxCodeFailures {
				if _, _, err := f.svc.LoginCode(ctx, pending, "000000", client); !errors.Is(err, ErrInvalidCredentials) {
					t.Fatalf("wrong code %d: %v", i, err)
				}
			}
		},
		"window over": func(f *sfFixture, _ *testing.T, _ string) {
			f.now = f.now.Add(DefaultSecondFactorWindow + time.Second)
		},
		"password changed": func(f *sfFixture, t *testing.T, _ string) {
			if _, err := f.st.DB.ExecContext(ctx, `UPDATE users SET password_hash = 'other' WHERE pseudonym = 'kiwi'`); err != nil {
				t.Fatal(err)
			}
		},
		"user locked": func(f *sfFixture, t *testing.T, _ string) {
			if _, err := f.st.DB.ExecContext(ctx, `UPDATE users SET disabled_at = 't' WHERE pseudonym = 'kiwi'`); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newSFFixture(t)
			f.user(t, storetest.User{Pseudonym: "kiwi"})
			key := f.enable(t, "kiwi")
			_, pending, _ := f.login(t, "kiwi", client)
			end(f, t, pending)
			if _, _, err := f.svc.LoginCode(ctx, pending, f.code(key), client); !errors.Is(err, ErrNoPending) {
				t.Errorf("step after the end: %v", err)
			}
		})
	}
	t.Run("lockout", func(t *testing.T) {
		f := newSFFixture(t)
		f.user(t, storetest.User{Pseudonym: "kiwi"})
		key := f.enable(t, "kiwi")
		f.set(t, SettingMaxFailures, "3")
		_, pending, _ := f.login(t, "kiwi", client)
		var locked *LockedError
		for i := range 4 {
			_, _, err := f.svc.LoginCode(ctx, pending, "000000", client)
			if errors.As(err, &locked) {
				break
			}
			if i == 3 {
				t.Fatalf("no lockout after wrong codes: %v", err)
			}
		}
		if _, _, err := f.svc.LoginCode(ctx, pending, f.code(key), client); !errors.Is(err, ErrNoPending) {
			t.Errorf("step after the lockout: %v", err)
		}
	})
}

// TestParallelCode: one waiting step and the right code sent many times at
// once give exactly one session.
func TestParallelCode(t *testing.T) {
	f := newSFFixture(t)
	f.user(t, storetest.User{Pseudonym: "kiwi"})
	key := f.enable(t, "kiwi")
	_, pending, _ := f.login(t, "kiwi", client)
	before := f.count(t, `SELECT count(*) FROM sessions`)
	c := f.code(key)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for range 10 {
		wg.Go(func() {
			if _, _, err := f.svc.LoginCode(context.Background(), pending, c, client); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if ok != 1 || f.count(t, `SELECT count(*) FROM sessions`) != before+1 {
		t.Errorf("sessions from one step: %d", ok)
	}
}

// TestParallelCodeOneLoginC6 covers AUTH-008: the same right code sent
// ten times at once gives exactly one session, also when the call that
// used the code is slow afterwards.
func TestParallelCodeOneLoginC6(t *testing.T) {
	f := newSFFixture(t)
	f.svc.codeTaken = func() { time.Sleep(50 * time.Millisecond) }
	f.user(t, storetest.User{Pseudonym: "kiwi"})
	key := f.enable(t, "kiwi")
	_, pending, _ := f.login(t, "kiwi", client)
	before := f.count(t, `SELECT count(*) FROM sessions`)
	c := f.code(key)
	var wg sync.WaitGroup
	var ok atomic.Int64
	for range 10 {
		wg.Go(func() {
			if _, _, err := f.svc.LoginCode(context.Background(), pending, c, client); err == nil {
				ok.Add(1)
			}
		})
	}
	wg.Wait()
	if ok.Load() != 1 || f.count(t, `SELECT count(*) FROM sessions`) != before+1 {
		t.Errorf("sessions from one step: %d", ok.Load())
	}
	if f.count(t, `SELECT count(*) FROM login_pending`) != 0 {
		t.Error("the open login outlived its use")
	}
}

// TestSelfServiceNeedsConfirmation covers AUTH-008 against a stolen
// session: setup, switching off and a new address need the password, and
// with the second factor on a code; setup is refused while one is on;
// switching off is refused while it is required.
func TestSelfServiceNeedsConfirmation(t *testing.T) {
	f := newSFFixture(t)
	f.user(t, storetest.User{Pseudonym: "kiwi"})
	ctx := context.Background()
	p, _, err := f.login(t, "kiwi", client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SetupTOTP(ctx, p, client, "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("setup without the password: %v", err)
	}
	if err := f.svc.SetEmail(ctx, p, client, "kiwi@example.org", "", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("address without the password: %v", err)
	}
	for _, bad := range []string{"no address", "Kiwi <kiwi@example.org>", "kiwi@example.org\r\nBcc: x@y.org", "kiwi\u202e@example.org", strings.Repeat("a", 250) + "@x.org"} {
		if err := f.svc.SetEmail(ctx, p, client, bad, testPassword, ""); !errors.Is(err, ErrInvalidEmail) {
			t.Errorf("address %.20q: %v", bad, err)
		}
	}
	if err := f.svc.SetEmail(ctx, p, client, "kiwi@example.org", testPassword, ""); err != nil {
		t.Fatal(err)
	}
	key := f.enable(t, "kiwi")
	if _, err := f.svc.SetupTOTP(ctx, p, client, testPassword); !errors.Is(err, ErrTOTPActive) {
		t.Errorf("setup while on: %v", err)
	}
	if err := f.svc.SetEmail(ctx, p, client, "other@example.org", testPassword, ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("address without the code: %v", err)
	}
	if err := f.svc.DisableTOTP(ctx, p, client, testPassword, "000000"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("off with a wrong code: %v", err)
	}
	f.set(t, SettingTOTPRequired, "true")
	if err := f.svc.DisableTOTP(ctx, p, client, testPassword, f.code(key)); !errors.Is(err, ErrTOTPRequired) {
		t.Errorf("off while required: %v", err)
	}
	f.set(t, SettingTOTPRequired, "false")
	if err := f.svc.DisableTOTP(ctx, p, client, testPassword, f.code(key)); err != nil {
		t.Errorf("off: %v", err)
	}
	if n := f.count(t, `SELECT count(*) FROM audit_log WHERE action IN ('totp.enable', 'totp.disable', 'profile.email')`); n != 3 {
		t.Errorf("audited = %d, want 3", n)
	}
	if !strings.Contains(f.logs.String(), `"event":"auth.totp.enabled"`) || !strings.Contains(f.logs.String(), `"event":"auth.totp.disabled"`) {
		t.Error("events missing")
	}
}

// TestRequiredRestrictsTheSession covers auth.totp_required: a user without
// a second factor gets a session that only sets it up, also on the
// WebSocket check; once set up, the session is free.
func TestRequiredRestrictsTheSession(t *testing.T) {
	f := newSFFixture(t)
	f.user(t, storetest.User{Pseudonym: "kiwi"})
	f.set(t, SettingTOTPRequired, "true")
	ctx := context.Background()
	token, p, err := f.svc.Login(ctx, "kiwi", testPassword, client)
	if err != nil || !p.TOTPSetupRequired {
		t.Fatalf("login = %+v %v", p, err)
	}
	if f.svc.SessionValid(ctx, p.SessionHash) {
		t.Error("restricted session valid for the WebSocket")
	}
	setup, err := f.svc.SetupTOTP(ctx, p, client, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setup.Secret)
	if err := f.svc.ConfirmTOTP(ctx, p, client, totp.Code(key, totp.Step(f.now))); err != nil {
		t.Fatal(err)
	}
	if q, err := f.svc.Authenticate(ctx, token); err != nil || q.TOTPSetupRequired || !f.svc.SessionValid(ctx, q.SessionHash) {
		t.Errorf("after the setup: %+v %v", q, err)
	}
}

// TestRecovery covers the recovery of AUTH-008: the same answer whether
// user and address exist; a link for a user with address and second
// factor; only the last link holds; it needs the password, holds once,
// resets only and ends all sessions; at most auth.recovery_per_hour; no
// secret, code, token or address in the log.
func TestRecovery(t *testing.T) {
	f := newSFFixture(t)
	f.user(t, storetest.User{Pseudonym: "kiwi"})
	ctx := context.Background()
	p, _, _ := f.login(t, "kiwi", client)
	if err := f.svc.SetEmail(ctx, p, client, "kiwi@example.org", testPassword, ""); err != nil {
		t.Fatal(err)
	}
	key := f.enable(t, "kiwi")
	// Each mail is sent in the background; waiting after each request
	// keeps the order of the links, so mails[0] is the older one.
	f.svc.RequestRecovery(ctx, "nobody", client)
	f.svc.RequestRecovery(ctx, "kiwi", client)
	f.svc.WaitMail()
	f.svc.RequestRecovery(ctx, "kiwi", client)
	f.svc.WaitMail()
	mails := f.mail.all()
	if len(mails) != 2 || mails[0].to != "kiwi@example.org" || !strings.HasPrefix(mails[0].link, "https://panel.example.org/recover#") {
		t.Fatalf("mails = %+v", mails)
	}
	first := strings.TrimPrefix(mails[0].link, "https://panel.example.org/recover#")
	last := strings.TrimPrefix(mails[1].link, "https://panel.example.org/recover#")
	if err := f.svc.RedeemRecovery(ctx, first, testPassword, client); !errors.Is(err, ErrRecoveryInvalid) {
		t.Errorf("older link: %v", err)
	}
	if err := f.svc.RedeemRecovery(ctx, last, "wrong", client); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password: %v", err)
	}
	if err := f.svc.RedeemRecovery(ctx, last, testPassword, client); err != nil {
		t.Fatal(err)
	}
	if f.count(t, `SELECT count(*) FROM users WHERE pseudonym = 'kiwi' AND totp_secret IS NULL`) != 1 ||
		f.count(t, `SELECT count(*) FROM sessions`) != 0 {
		t.Error("recovery did not reset or left sessions")
	}
	if err := f.svc.RedeemRecovery(ctx, last, testPassword, client); !errors.Is(err, ErrRecoveryInvalid) {
		t.Errorf("link used twice: %v", err)
	}
	// Limit per user and hour: 3 requests, the 4th sends nothing.
	f.enable(t, "kiwi")
	f.mail.sent = nil
	f.now = f.now.Add(time.Hour)
	for range DefaultRecoveryPerHour + 1 {
		f.svc.RequestRecovery(ctx, "kiwi", Client{IP: client.IP.Next(), RequestID: "r"})
	}
	f.svc.WaitMail()
	if n := len(f.mail.all()); n != DefaultRecoveryPerHour {
		t.Errorf("mails within the hour = %d, want %d", n, DefaultRecoveryPerHour)
	}
	// A link ends with the expiry.
	f.now = f.now.Add(2 * time.Hour)
	f.svc.RequestRecovery(ctx, "kiwi", Client{IP: client.IP.Next().Next(), RequestID: "r"})
	f.svc.WaitMail()
	all := f.mail.all()
	stale := strings.TrimPrefix(all[len(all)-1].link, "https://panel.example.org/recover#")
	f.now = f.now.Add(DefaultRecoveryLifetime + time.Second)
	if err := f.svc.RedeemRecovery(ctx, stale, testPassword, client); !errors.Is(err, ErrRecoveryInvalid) {
		t.Errorf("expired link: %v", err)
	}
	logs := f.logs.String()
	for _, secretText := range []string{base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key), first, last, stale, "kiwi@"} {
		if strings.Contains(logs, secretText) {
			t.Errorf("%q in the log", secretText)
		}
	}
	if !strings.Contains(logs, `"event":"auth.recovery.requested"`) || !strings.Contains(logs, `"event":"auth.recovery.used"`) {
		t.Error("recovery events missing")
	}
}

// TestNoRecoveryLinkWithoutPublicURL: without RCD_PUBLIC_URL no recovery
// mail goes out, the answer stays the same.
func TestNoRecoveryLinkWithoutPublicURL(t *testing.T) {
	f := newSFFixture(t)
	f.svc.sf.cfg.LinkBase = ""
	f.user(t, storetest.User{Pseudonym: "kiwi"})
	ctx := context.Background()
	p, _, _ := f.login(t, "kiwi", client)
	_ = f.svc.SetEmail(ctx, p, client, "kiwi@example.org", testPassword, "")
	f.enable(t, "kiwi")
	f.svc.RequestRecovery(ctx, "kiwi", client)
	f.svc.WaitMail()
	if n := len(f.mail.all()); n != 0 {
		t.Errorf("mails without public URL = %d", n)
	}
}
