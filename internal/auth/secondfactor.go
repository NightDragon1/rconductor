// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"net/netip"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/auth/password"
	"github.com/NightDragon1/rconductor/internal/auth/totp"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/netlimit"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/secret"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// Settings of the second factor (AUTH-008, Design 5.6).
const (
	SettingTOTPRequired       = "auth.totp_required"
	SettingSecondFactorWindow = "auth.second_factor_window"
	DefaultSecondFactorWindow = 5 * time.Minute
	MinSecondFactorWindow     = time.Minute
	SettingRecoveryLifetime   = "auth.recovery_lifetime"
	DefaultRecoveryLifetime   = 30 * time.Minute
	MinRecoveryLifetime       = 5 * time.Minute
	SettingRecoveryPerHour    = "auth.recovery_per_hour"
	DefaultRecoveryPerHour    = 3
	MinRecoveryPerHour        = 1
)

// Fixed limits of the second factor.
const (
	// MaxCodeFailures ends a waiting second step.
	MaxCodeFailures = 5
	maxEmail        = 254
	maxRecoveryKeys = 10000
	issuer          = "RCONductor"
)

// Errors of the second factor.
var (
	// ErrSecondFactorRequired means the password was right and the code
	// must follow (AUTH-008).
	ErrSecondFactorRequired = errors.New("second factor required")
	// ErrNoPending means no waiting second step: none, expired, used up.
	ErrNoPending = errors.New("no waiting sign-in, start again")
	// ErrTOTPActive refuses a setup while the second factor is on.
	ErrTOTPActive = errors.New("two-factor sign-in is on already")
	// ErrTOTPRequired refuses switching it off while it is required.
	ErrTOTPRequired = errors.New("two-factor sign-in is required for everyone")
	// ErrInvalidEmail refuses an address.
	ErrInvalidEmail = errors.New("not a plain mail address")
	// ErrRecoveryInvalid means the recovery link does not hold.
	ErrRecoveryInvalid = errors.New("the recovery link is not valid")
)

// Mailer sends the recovery mail; it returns the category of a failure or
// "" (NFR-017). The link carries the token in its fragment.
type Mailer interface {
	SendRecovery(ctx context.Context, to, link string) string
}

// SecondFactor holds what the second factor needs besides the service.
type SecondFactor struct {
	Box *secret.Box
	// Mail sends the recovery mail; nil sends none.
	Mail Mailer
	// LinkBase is the panel's public URL, for the recovery link. Without
	// it no recovery mail goes out: a link built from the request could
	// point to any host the requester names.
	LinkBase string
}

type recoveryWindow struct {
	start time.Time
	count int
}

type secondFactorState struct {
	cfg SecondFactor
	mu  sync.Mutex
	// clients counts recovery requests per client (an IPv6 client per /64)
	// in the current hour; at most maxRecoveryKeys of them (D-076).
	clients map[netip.Addr]*recoveryWindow
	// users counts recovery requests per user in the current hour. Only
	// users with an address and the second factor get a window, so the
	// users table bounds it; it is never cleared early.
	users map[string]*recoveryWindow
	// full is set while a full client table refuses new clients; the
	// warning goes out once per such spell.
	full bool
	// send delivers recovery mail in the background; tests wait on it.
	send sync.WaitGroup
}

// SetSecondFactor enables the second factor (AUTH-008).
func (s *Service) SetSecondFactor(cfg SecondFactor) {
	s.sf = &secondFactorState{cfg: cfg, clients: map[netip.Addr]*recoveryWindow{}, users: map[string]*recoveryWindow{}}
}

// WaitMail waits for recovery mail sent in the background, for tests and
// the shutdown.
func (s *Service) WaitMail() {
	if s.sf != nil {
		s.sf.send.Wait()
	}
}

// EnsureSecondFactorDefaults stores the defaults of the settings.
func EnsureSecondFactorDefaults(ctx context.Context, st *store.Store) error {
	for key, v := range map[string]any{
		SettingTOTPRequired:       false,
		SettingSecondFactorWindow: DefaultSecondFactorWindow.String(),
		SettingRecoveryLifetime:   DefaultRecoveryLifetime.String(),
		SettingRecoveryPerHour:    DefaultRecoveryPerHour,
	} {
		if _, err := st.EnsureDefault(ctx, key, v); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) durationSetting(ctx context.Context, key string, def, low time.Duration) time.Duration {
	var v string
	if err := s.st.GlobalSetting(ctx, key, &v); err != nil {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return max(d, low)
}

// TOTPRequired reports whether the RCON admin requires the second factor
// for everyone.
func (s *Service) TOTPRequired(ctx context.Context) bool {
	var on bool
	_ = s.st.GlobalSetting(ctx, SettingTOTPRequired, &on)
	return on
}

func secretContext(userID string) secret.Context {
	return secret.Context{Table: "users", ID: userID, Column: "totp_secret"}
}

func newToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// guarded runs one check of an account under the login protection
// (SEC-004): a locked account or client is refused, the attempt counts
// before its result, a failure counts to the lockout and a success clears
// it. check reports whether the input was right.
func (s *Service) guarded(ctx context.Context, userID, pseudonym string, c Client, cause string, check func() (bool, error)) error {
	key := accountKey(pseudonym)
	lim := s.loginPolicy(ctx)
	delay, locked := s.throttle.check(key, c.IP, s.now(), lim.lockout)
	if locked > 0 {
		if s.throttle.firstRefused(key, c.IP, s.now()) {
			s.loginDenied(ctx, c, userID, pseudonym, "rate_limited", "locked")
		}
		return &LockedError{RetryAfter: locked}
	}
	tk, ok := s.throttle.reserve(key, c.IP, s.now(), lim)
	if !ok {
		s.reportFull(ctx, c)
		return &LockedError{RetryAfter: time.Second}
	}
	defer s.throttle.release(key, c.IP, tk)
	if tk.place != nil {
		d, turn, err := s.throttle.wait(ctx, tk.place, key, s.sleep, s.now, lim.lockout, lim.gap())
		s.reportFull(ctx, c)
		if err != nil {
			return err
		}
		if !turn {
			return &LockedError{RetryAfter: time.Second}
		}
		delay = max(delay, d)
	}
	if delay > 0 {
		if err := s.sleep(ctx, delay); err != nil {
			return err
		}
	}
	right, err := check()
	if err != nil {
		return err
	}
	if !right {
		s.loginDenied(ctx, c, userID, pseudonym, "unauthenticated", cause)
		pairLocked, clientLocked := s.throttle.fail(key, c.IP, s.now(), lim, tk)
		if pairLocked {
			s.lockedOut(ctx, c, userID, pseudonym, "account", lim.lockout)
		}
		if clientLocked {
			s.lockedOut(ctx, c, userID, pseudonym, "client", lim.lockout)
		}
		return ErrInvalidCredentials
	}
	s.throttle.success(key, c.IP, s.now(), lim.lockout)
	return nil
}

// startSecondStep keeps a login that passed the password until its code
// comes: a token for the cookie rcd_pending, one use, bound to the user.
func (s *Service) startSecondStep(ctx context.Context, u sqlitedb.GetLoginUserRow, c Client) (string, time.Time, error) {
	token, err := newToken()
	if err != nil {
		return "", time.Time{}, err
	}
	now := s.now().UTC()
	window := s.durationSetting(ctx, SettingSecondFactorWindow, DefaultSecondFactorWindow, MinSecondFactorWindow)
	expires := now.Add(window)
	ua := c.UserAgent
	if len(ua) > maxUserAgent {
		ua = ua[:maxUserAgent]
	}
	_ = s.st.Queries.DeleteExpiredLoginPending(ctx, now.Format(store.TimeLayout))
	if err := s.st.Queries.InsertLoginPending(ctx, sqlitedb.InsertLoginPendingParams{
		IDHash: HashToken(token), UserID: u.ID, CreatedAt: now.Format(store.TimeLayout),
		ExpiresAt: expires.Format(store.TimeLayout), Ip: c.IP.String(), UserAgent: ua,
	}); err != nil {
		return "", time.Time{}, fmt.Errorf("keep second step: %w", err)
	}
	return token, expires, nil
}

// SecondStepError tells the web layer the token of the waiting step.
type SecondStepError struct {
	Token     string
	ExpiresAt time.Time
}

func (e *SecondStepError) Error() string { return ErrSecondFactorRequired.Error() }

// Unwrap lets errors.Is find ErrSecondFactorRequired.
func (e *SecondStepError) Unwrap() error { return ErrSecondFactorRequired }

// LoginCode completes a login with the code of the second factor. The
// waiting step holds once and for auth.second_factor_window; it ends after
// MaxCodeFailures wrong codes, a lockout, a lock of the user or a change of
// the password. Wrong codes count to login.max_failures. Only now the
// session starts, and a session sent along ends (D-052).
func (s *Service) LoginCode(ctx context.Context, pending, code string, c Client) (string, Principal, error) {
	if s.sf == nil || pending == "" {
		return "", Principal{}, ErrNoPending
	}
	hash := HashToken(pending)
	row, err := s.st.Queries.GetLoginPending(ctx, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", Principal{}, ErrNoPending
	}
	if err != nil {
		return "", Principal{}, err
	}
	expires, err := time.Parse(store.TimeLayout, row.ExpiresAt)
	if err != nil || !s.now().UTC().Before(expires) || row.DisabledAt.Valid || len(row.TotpSecret) == 0 {
		_, _ = s.st.Queries.DeleteLoginPending(ctx, hash)
		return "", Principal{}, ErrNoPending
	}
	key, err := s.sf.cfg.Box.Open(row.TotpSecret, secretContext(row.UserID))
	if err != nil {
		return "", Principal{}, fmt.Errorf("open second factor: %w", err)
	}
	gone := false
	err = s.guarded(ctx, row.UserID, row.Pseudonym, c, "wrong_code", func() (bool, error) {
		step, ok := totp.Verify([]byte(key), code, s.now())
		if !ok {
			return false, nil
		}
		won, missing, err := s.takeCode(ctx, row.UserID, step, hash)
		gone = missing
		return won || missing, err
	})
	var locked *LockedError
	switch {
	case errors.As(err, &locked):
		_, _ = s.st.Queries.DeleteLoginPending(ctx, hash)
		return "", Principal{}, err
	case errors.Is(err, ErrInvalidCredentials):
		n, ferr := s.st.Queries.AddPendingFailure(ctx, hash)
		if ferr == nil && n >= MaxCodeFailures {
			_, _ = s.st.Queries.DeleteLoginPending(ctx, hash)
		}
		return "", Principal{}, err
	case err != nil:
		return "", Principal{}, err
	case gone:
		return "", Principal{}, ErrNoPending
	}
	if s.codeTaken != nil {
		s.codeTaken()
	}
	u := sqlitedb.GetLoginUserRow{ID: row.UserID, Pseudonym: row.Pseudonym, Role: row.Role, PasswordHash: row.PasswordHash,
		IsEmergency: row.IsEmergency, DisabledAt: row.DisabledAt, MustChangePassword: row.MustChangePassword, TotpEnabled: true}
	token, p, err := s.createSession(ctx, u, c)
	if err != nil {
		return "", Principal{}, err
	}
	s.log.Info("login", logging.Event(logging.AuthLoginOK),
		slog.String(logging.KeyRequestID, c.RequestID), slog.String(logging.KeySession, p.SessionRef()),
		slog.String(logging.KeyUser, p.Pseudonym), slog.String(logging.KeyRole, p.Role), slog.String("factor", "totp"))
	return token, p, nil
}

// takeCode uses a TOTP step and takes the open login in one transaction
// (D-075): a code holds once, and whoever uses it has the login. won is
// false when the step was used before; missing is true when the open
// login is gone, the step then stays unused.
func (s *Service) takeCode(ctx context.Context, userID string, step int64, hash string) (won, missing bool, err error) {
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, false, err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	n, err := q.UseTOTPStep(ctx, sqlitedb.UseTOTPStepParams{Step: step, ID: userID})
	if err != nil || n != 1 {
		return false, false, err
	}
	d, err := q.DeleteLoginPending(ctx, hash)
	if err != nil {
		return false, false, err
	}
	if d != 1 {
		return false, true, nil
	}
	return true, false, tx.Commit()
}

// SecondFactorView is the own state of the second factor.
type SecondFactorView struct {
	Enabled  bool
	Required bool
	Email    string
}

// SecondFactorOf returns the state of the caller.
func (s *Service) SecondFactorOf(ctx context.Context, p Principal) (SecondFactorView, error) {
	r, err := s.st.Queries.GetSecondFactor(ctx, p.UserID)
	if err != nil {
		return SecondFactorView{}, err
	}
	return SecondFactorView{Enabled: len(r.TotpSecret) > 0, Required: s.TOTPRequired(ctx), Email: r.Email}, nil
}

// confirmSelf checks password and, with the second factor on, a code of
// the caller: a session alone must not lock its owner out (AUTH-008).
// Wrong input counts to the login protection.
func (s *Service) confirmSelf(ctx context.Context, p Principal, c Client, pw, code string) (sqlitedb.GetSecondFactorRow, error) {
	r, err := s.st.Queries.GetSecondFactor(ctx, p.UserID)
	if err != nil {
		return r, err
	}
	err = s.guarded(ctx, p.UserID, p.Pseudonym, c, "wrong_confirmation", func() (bool, error) {
		if len(pw) > 4*password.MaxLength {
			pw = ""
		}
		ok, err := s.hasher.Verify(ctx, r.PasswordHash, pw)
		if err != nil || !ok {
			return false, err
		}
		if len(r.TotpSecret) == 0 {
			return true, nil
		}
		key, err := s.sf.cfg.Box.Open(r.TotpSecret, secretContext(p.UserID))
		if err != nil {
			return false, fmt.Errorf("open second factor: %w", err)
		}
		step, ok := totp.Verify([]byte(key), code, s.now())
		if !ok {
			return false, nil
		}
		n, err := s.st.Queries.UseTOTPStep(ctx, sqlitedb.UseTOTPStepParams{Step: step, ID: p.UserID})
		return n == 1, err
	})
	return r, err
}

// Setup is a new secret before its first code.
type Setup struct {
	Secret string // base32, to type in
	URI    string // otpauth URI, for the QR code
}

// SetupTOTP starts the second factor of the caller: password required,
// refused while one is on. The secret waits sealed until ConfirmTOTP.
func (s *Service) SetupTOTP(ctx context.Context, p Principal, c Client, pw string) (Setup, error) {
	if s.sf == nil {
		return Setup{}, errors.New("second factor not available")
	}
	if cur, err := s.st.Queries.GetSecondFactor(ctx, p.UserID); err != nil {
		return Setup{}, err
	} else if len(cur.TotpSecret) > 0 {
		return Setup{}, ErrTOTPActive
	}
	if _, err := s.confirmSelf(ctx, p, c, pw, ""); err != nil {
		return Setup{}, err
	}
	key, err := totp.NewSecret()
	if err != nil {
		return Setup{}, err
	}
	sealed, err := s.sf.cfg.Box.Seal(logging.Secret(key), secretContext(p.UserID))
	if err != nil {
		return Setup{}, err
	}
	n, err := s.st.Queries.SetTOTPPending(ctx, sqlitedb.SetTOTPPendingParams{TotpPending: sealed, UpdatedAt: store.Now(), ID: p.UserID})
	if err != nil {
		return Setup{}, err
	}
	if n == 0 {
		return Setup{}, ErrTOTPActive
	}
	return Setup{Secret: totp.Encode(key), URI: totp.URI(issuer, p.Pseudonym, key)}, nil
}

// ConfirmTOTP switches the second factor on with the first code of the
// new secret; audited (auth.totp.enabled).
func (s *Service) ConfirmTOTP(ctx context.Context, p Principal, c Client, code string) error {
	r, err := s.st.Queries.GetSecondFactor(ctx, p.UserID)
	if err != nil {
		return err
	}
	if len(r.TotpSecret) > 0 {
		return ErrTOTPActive
	}
	if len(r.TotpPending) == 0 {
		return ErrNoPending
	}
	var step int64
	err = s.guarded(ctx, p.UserID, p.Pseudonym, c, "wrong_code", func() (bool, error) {
		key, err := s.sf.cfg.Box.Open(r.TotpPending, secretContext(p.UserID))
		if err != nil {
			return false, fmt.Errorf("open second factor: %w", err)
		}
		var ok bool
		step, ok = totp.Verify([]byte(key), code, s.now())
		return ok, nil
	})
	if err != nil {
		return err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	n, err := sqlitedb.New(tx).ActivateTOTP(ctx, sqlitedb.ActivateTOTPParams{TotpLastStep: step, UpdatedAt: store.Now(), ID: p.UserID})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTOTPActive
	}
	if _, err := audit.Write(ctx, tx, audit.Entry{
		Kind: audit.KindAuth, ActorID: p.UserID, ActorRole: p.Role, SessionID: p.SessionHash,
		TargetType: "user", TargetID: p.UserID, Action: "totp.enable", Result: audit.ResultOK,
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.log.Info("second factor on", logging.Event(logging.AuthTOTPEnabled), slog.String(logging.KeyRequestID, c.RequestID),
		slog.String(logging.KeySession, p.SessionRef()), slog.String(logging.KeyUser, p.Pseudonym))
	return nil
}

// DisableTOTP switches the own second factor off with password and code;
// refused while it is required.
func (s *Service) DisableTOTP(ctx context.Context, p Principal, c Client, pw, code string) error {
	if s.TOTPRequired(ctx) {
		return ErrTOTPRequired
	}
	r, err := s.confirmSelf(ctx, p, c, pw, code)
	if err != nil {
		return err
	}
	if len(r.TotpSecret) == 0 {
		return ErrNoPending
	}
	return s.clearTOTP(ctx, p.UserID, p.Pseudonym, p.UserID, p.Role, p.SessionHash, c.RequestID, "self")
}

// ClearTOTP resets the second factor of a user for an administrator, who
// checked the right to manage the user before (AUTH-008); the sessions of
// the user end.
func (s *Service) ClearTOTP(ctx context.Context, userID, pseudonym, actorID, actorRole, sessionHash, requestID string) error {
	if err := s.clearTOTP(ctx, userID, pseudonym, actorID, actorRole, sessionHash, requestID, "admin_reset"); err != nil {
		return err
	}
	return s.RevokeUser(ctx, userID, "totp_reset", requestID)
}

func (s *Service) clearTOTP(ctx context.Context, userID, pseudonym, actorID, actorRole, sessionHash, requestID, cause string) error {
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	n, err := sqlitedb.New(tx).ClearTOTP(ctx, sqlitedb.ClearTOTPParams{UpdatedAt: store.Now(), ID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	if _, err := audit.Write(ctx, tx, audit.Entry{
		Kind: audit.KindAuth, ActorID: actorID, ActorRole: actorRole, SessionID: sessionHash,
		TargetType: "user", TargetID: userID, Action: "totp.disable", Result: audit.ResultOK,
		Payload: map[string]string{"cause": cause},
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.log.Info("second factor off", logging.Event(logging.AuthTOTPDisabled), slog.String(logging.KeyRequestID, requestID),
		slog.String(logging.KeyUser, pseudonym), slog.String("cause", cause))
	return nil
}

// hasControl reports control or format characters (no header injection,
// no bidi tricks).
func hasControl(v string) bool {
	return strings.IndexFunc(v, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) >= 0
}

// SetEmail changes the own address for the recovery mail: password and,
// with the second factor on, a code required; empty removes it.
func (s *Service) SetEmail(ctx context.Context, p Principal, c Client, email, pw, code string) error {
	email = strings.TrimSpace(email)
	if email != "" {
		a, err := mail.ParseAddress(email)
		if len(email) > maxEmail || hasControl(email) || err != nil || a.Name != "" || a.Address != email {
			return ErrInvalidEmail
		}
	}
	if _, err := s.confirmSelf(ctx, p, c, pw, code); err != nil {
		return err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := sqlitedb.New(tx).SetEmail(ctx, sqlitedb.SetEmailParams{Email: email, UpdatedAt: store.Now(), ID: p.UserID}); err != nil {
		return err
	}
	if _, err := audit.Write(ctx, tx, audit.Entry{
		Kind: audit.KindAuth, ActorID: p.UserID, ActorRole: p.Role, SessionID: p.SessionHash,
		TargetType: "user", TargetID: p.UserID, Action: "profile.email", Result: audit.ResultOK,
		Payload: map[string]bool{"set": email != ""},
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// recoveryLimit is auth.recovery_per_hour.
func (s *Service) recoveryLimit(ctx context.Context) int {
	var v int
	if err := s.st.GlobalSetting(ctx, SettingRecoveryPerHour, &v); err == nil {
		return max(v, MinRecoveryPerHour)
	}
	return DefaultRecoveryPerHour
}

// takeWindow spends one request of w, opening a new hour when the last
// one ended.
func takeWindow(w *recoveryWindow, now time.Time, limit int) bool {
	if now.Sub(w.start) >= time.Hour {
		*w = recoveryWindow{start: now}
	}
	if w.count >= limit {
		return false
	}
	w.count++
	return true
}

// takeRecoveryClient counts a request of the client. A full table drops
// only windows whose hour ended; if it stays full, a new client counts as
// over its limit, while known clients and all user windows keep their
// counts (D-076).
func (s *Service) takeRecoveryClient(ctx context.Context, ip netip.Addr) bool {
	limit := s.recoveryLimit(ctx)
	key := netlimit.ClientKey(ip)
	now := s.now()
	s.sf.mu.Lock()
	defer s.sf.mu.Unlock()
	w := s.sf.clients[key]
	if w == nil {
		if len(s.sf.clients) >= maxRecoveryKeys {
			for k, old := range s.sf.clients {
				if now.Sub(old.start) >= time.Hour {
					delete(s.sf.clients, k)
				}
			}
		}
		if len(s.sf.clients) >= maxRecoveryKeys {
			if !s.sf.full {
				s.sf.full = true
				s.log.Warn("recovery limit table full, requests of new clients are refused", slog.Int("limit", maxRecoveryKeys))
			}
			return false
		}
		s.sf.full = false
		w = &recoveryWindow{start: now}
		s.sf.clients[key] = w
	}
	return takeWindow(w, now, limit)
}

// takeRecoveryUser counts a request for the user.
func (s *Service) takeRecoveryUser(ctx context.Context, userID string) bool {
	limit := s.recoveryLimit(ctx)
	now := s.now()
	s.sf.mu.Lock()
	defer s.sf.mu.Unlock()
	w := s.sf.users[userID]
	if w == nil {
		w = &recoveryWindow{start: now}
		s.sf.users[userID] = w
	}
	return takeWindow(w, now, limit)
}

func domainOf(addr string) string {
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		return strings.ToLower(addr[i+1:])
	}
	return ""
}

// RequestRecovery answers every request alike: whether user and address
// exist shows neither in the answer nor in its time. For a user with an
// address and the second factor on, within auth.recovery_per_hour per user
// and per client, a new link replaces the last one and goes out in the
// background (AUTH-008, NFR-017).
func (s *Service) RequestRecovery(ctx context.Context, pseudonym string, c Client) {
	if s.sf == nil || len(pseudonym) > 4*maxAccountKey {
		return
	}
	clientOK := s.takeRecoveryClient(ctx, c.IP)
	u, err := s.st.Queries.GetRecoveryTarget(ctx, pseudonym)
	if err != nil || !clientOK || u.DisabledAt.Valid || !u.TotpEnabled || u.Email == "" {
		return
	}
	if !s.takeRecoveryUser(ctx, u.ID) {
		return
	}
	token, err := newToken()
	if err != nil {
		return
	}
	now := s.now().UTC()
	life := s.durationSetting(ctx, SettingRecoveryLifetime, DefaultRecoveryLifetime, MinRecoveryLifetime)
	if err := s.st.Queries.PutRecovery(ctx, sqlitedb.PutRecoveryParams{
		UserID: u.ID, TokenHash: HashToken(token), CreatedAt: now.Format(store.TimeLayout),
		ExpiresAt: now.Add(life).Format(store.TimeLayout),
	}); err != nil {
		s.log.Error("recovery not stored", slog.Any("err", err))
		return
	}
	s.audit(ctx, audit.Entry{
		Kind: audit.KindAuth, TargetType: "user", TargetID: u.ID, Action: "totp.recovery_request", Result: audit.ResultOK,
		Payload: map[string]string{"request_id": c.RequestID, "domain": domainOf(u.Email)},
	})
	s.log.Info("recovery requested", logging.Event(logging.AuthRecoveryRequested), slog.String(logging.KeyRequestID, c.RequestID),
		slog.String("user_id", u.ID), slog.String("domain", domainOf(u.Email)))
	if s.sf.cfg.Mail == nil || s.sf.cfg.LinkBase == "" {
		s.log.Warn("recovery mail not sent: no SMTP or no RCD_PUBLIC_URL", slog.String(logging.KeyRequestID, c.RequestID))
		return
	}
	link := strings.TrimRight(s.sf.cfg.LinkBase, "/") + "/recover#" + token
	to, requestID := u.Email, c.RequestID
	s.sf.send.Go(func() {
		mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		if category := s.sf.cfg.Mail.SendRecovery(mctx, to, link); category != "" {
			s.log.Warn("recovery mail not sent", slog.String(logging.KeyRequestID, requestID), slog.String(logging.KeyReason, category))
		}
	})
}

// RedeemRecovery resets the second factor with the link and the password.
// It does not sign in: all sessions of the user end, the next login needs
// the password (AUTH-008). Wrong passwords count to the lockout; the link
// holds once.
func (s *Service) RedeemRecovery(ctx context.Context, token, pw string, c Client) error {
	if s.sf == nil || token == "" || len(token) > 128 {
		return ErrRecoveryInvalid
	}
	hash := HashToken(token)
	r, err := s.st.Queries.GetRecovery(ctx, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRecoveryInvalid
	}
	if err != nil {
		return err
	}
	expires, err := time.Parse(store.TimeLayout, r.ExpiresAt)
	if err != nil || !s.now().UTC().Before(expires) || r.DisabledAt.Valid {
		_, _ = s.st.Queries.TakeRecovery(ctx, sqlitedb.TakeRecoveryParams{TokenHash: hash, UserID: r.UserID})
		return ErrRecoveryInvalid
	}
	err = s.guarded(ctx, r.UserID, r.Pseudonym, c, "wrong_password", func() (bool, error) {
		if len(pw) > 4*password.MaxLength {
			pw = ""
		}
		return s.hasher.Verify(ctx, r.PasswordHash, pw)
	})
	if err != nil {
		return err
	}
	if n, err := s.st.Queries.TakeRecovery(ctx, sqlitedb.TakeRecoveryParams{TokenHash: hash, UserID: r.UserID}); err != nil || n != 1 {
		return ErrRecoveryInvalid
	}
	s.log.Info("recovery used", logging.Event(logging.AuthRecoveryUsed), slog.String(logging.KeyRequestID, c.RequestID),
		slog.String(logging.KeyUser, r.Pseudonym))
	if err := s.clearTOTP(ctx, r.UserID, r.Pseudonym, r.UserID, r.Role, "", c.RequestID, "recovery"); err != nil {
		return err
	}
	return s.RevokeUser(ctx, r.UserID, "totp_recovery", c.RequestID)
}
