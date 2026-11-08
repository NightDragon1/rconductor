// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/auth/password"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// Settings keys of the session policy and their defaults (decision M1).
const (
	SettingIdleTimeout = "session.idle_timeout"
	SettingMaxLifetime = "session.max_lifetime"

	DefaultIdleTimeout = 12 * time.Hour
	DefaultMaxLifetime = 7 * 24 * time.Hour
)

// touchInterval limits writes of last_seen_at to one per minute and session.
const touchInterval = time.Minute

// maxUserAgent bounds the stored user agent (SEC-006: client data).
const maxUserAgent = 256

var (
	// ErrInvalidCredentials answers every failed login alike: unknown
	// user, wrong password, disabled account, disabled emergency admin.
	ErrInvalidCredentials = errors.New("invalid pseudonym or password")
	// ErrUnauthenticated means no valid session.
	ErrUnauthenticated = errors.New("no valid session")
)

// LockedError means the account is locked for this client address after
// too many failed logins (SEC-004).
type LockedError struct{ RetryAfter time.Duration }

func (e *LockedError) Error() string { return "too many failed logins, try again later" }

// Principal is the authenticated user of a request. Rights are not part of
// it; they are computed per request (Design 2.1).
type Principal struct {
	UserID      string
	Pseudonym   string
	Role        string
	IsEmergency bool
	// MustChangePassword allows only changing the password, logging out
	// and reading the session (AUTH-010).
	MustChangePassword bool
	// TOTPSetupRequired allows only setting up the second factor, logging
	// out and reading the session, while the RCON admin requires it and
	// the user has none (AUTH-008).
	TOTPSetupRequired bool
	// SessionHash identifies the session in the database and the audit log.
	SessionHash string
	ExpiresAt   time.Time
}

// SessionRef is the short session hash for logs (Design 8.2).
func (p Principal) SessionRef() string { return p.SessionHash[:8] }

// Client is the origin of a login, for the session record and the audit.
type Client struct {
	IP        netip.Addr
	UserAgent string
	RequestID string
	// SessionToken is the session cookie the browser sent along with the
	// login, if any. A login ends that session when it belongs to the same
	// user (AUTH-006, D-052). Never logged (SEC-012).
	SessionToken string
}

// Service logs users in and out and resolves session cookies.
type Service struct {
	st        *store.Store
	hasher    *password.Hasher
	log       *slog.Logger
	emergency bool
	throttle  *throttle
	now       func() time.Time
	sleep     func(ctx context.Context, d time.Duration) error
	// minTracked is the lowest table size; tests go below it.
	minTracked int

	mu    sync.Mutex
	ended []func(sessionHashes []string)

	// sf is the second factor (AUTH-008); nil without it.
	sf *secondFactorState
}

// OnSessionsEnded registers f to learn about every ended session, so that
// open WebSockets of it close at once (AUTH-006).
func (s *Service) OnSessionsEnded(f func(sessionHashes []string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ended = append(s.ended, f)
}

func (s *Service) sessionsEnded(hashes ...string) {
	if len(hashes) == 0 {
		return
	}
	s.mu.Lock()
	fs := append([]func([]string){}, s.ended...)
	s.mu.Unlock()
	for _, f := range fs {
		f(hashes)
	}
}

// NewService returns the authentication service. emergencyEnabled is
// RCD_EMERGENCY_ADMIN (AUTH-003).
func NewService(st *store.Store, h *password.Hasher, log *slog.Logger, emergencyEnabled bool) *Service {
	return &Service{
		st: st, hasher: h, log: log, emergency: emergencyEnabled, throttle: newThrottle(),
		now: time.Now, sleep: sleepCtx, minTracked: minTracked,
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// EnsureDefaults stores the default session settings unless they exist.
func EnsureDefaults(ctx context.Context, st *store.Store) error {
	for key, v := range map[string]any{
		SettingIdleTimeout:       DefaultIdleTimeout.String(),
		SettingMaxLifetime:       DefaultMaxLifetime.String(),
		SettingMaxFailures:       DefaultMaxFailures,
		SettingMaxClientFailures: DefaultMaxClientFailures,
		SettingLockout:           DefaultLockout.String(),
		SettingMaxTracked:        DefaultMaxTracked,
		SettingOverflowLine:      overflowLine,
		SettingOverflowGap:       overflowGap.String(),
	} {
		if _, err := st.EnsureDefault(ctx, key, v); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) policy(ctx context.Context) (idle, maxLife time.Duration) {
	idle, maxLife = DefaultIdleTimeout, DefaultMaxLifetime
	for key, dst := range map[string]*time.Duration{SettingIdleTimeout: &idle, SettingMaxLifetime: &maxLife} {
		var v string
		if err := s.st.GlobalSetting(ctx, key, &v); err != nil {
			continue
		}
		if d, err := time.ParseDuration(v); err == nil && d >= time.Minute {
			*dst = d
		} else {
			s.log.Warn("invalid session setting, using default", slog.String("setting", key))
		}
	}
	return idle, maxLife
}

// loginPolicy reads the login protection settings.
func (s *Service) loginPolicy(ctx context.Context) loginLimits {
	lim := loginLimits{maxFailures: DefaultMaxFailures, maxClientFailures: DefaultMaxClientFailures, lockout: DefaultLockout, maxTracked: DefaultMaxTracked}
	ints := map[string]*int{SettingMaxFailures: &lim.maxFailures, SettingMaxClientFailures: &lim.maxClientFailures, SettingMaxTracked: &lim.maxTracked, SettingOverflowLine: &lim.overflowLine}
	for key, dst := range ints {
		var n int
		if err := s.st.GlobalSetting(ctx, key, &n); err != nil {
			continue
		}
		if n >= 1 {
			*dst = n
		} else {
			s.log.Warn("invalid login setting, using default", slog.String("setting", key))
		}
	}
	if lim.maxTracked < s.minTracked {
		s.log.Warn("invalid login setting, using default", slog.String("setting", SettingMaxTracked))
		lim.maxTracked = DefaultMaxTracked
	}
	var v string
	if err := s.st.GlobalSetting(ctx, SettingLockout, &v); err == nil {
		if d, err := time.ParseDuration(v); err == nil && d >= time.Minute {
			lim.lockout = d
		} else {
			s.log.Warn("invalid login setting, using default", slog.String("setting", SettingLockout))
		}
	}
	if err := s.st.GlobalSetting(ctx, SettingOverflowGap, &v); err == nil {
		if d, err := time.ParseDuration(v); err == nil && d >= minOverflowGap {
			lim.overflowGap = d
		} else {
			s.log.Warn("invalid login setting, using default", slog.String("setting", SettingOverflowGap))
		}
	}
	return lim
}

// MaxLifetime is the configured maximum session lifetime, also used for
// the cookie.
func (s *Service) MaxLifetime(ctx context.Context) time.Duration {
	_, maxLife := s.policy(ctx)
	return maxLife
}

// HashToken returns the stored form of a session token (Design 4.1).
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Login checks pseudonym and password and starts a session. It returns the
// session token for the cookie. Every failure returns ErrInvalidCredentials
// after the same Argon2id work, or password.ErrBusy.
func (s *Service) Login(ctx context.Context, pseudonym, pw string, c Client) (string, Principal, error) {
	key := accountKey(pseudonym)
	lim := s.loginPolicy(ctx)
	delay, locked := s.throttle.check(key, c.IP, s.now(), lim.lockout)

	u, err := s.st.Queries.GetLoginUser(ctx, pseudonym)
	known := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", Principal{}, fmt.Errorf("look up user: %w", err)
	}
	if locked > 0 {
		// Without a session anyone can repeat this at HTTP speed: one
		// audit row per lockout, not per attempt (T-016).
		if s.throttle.firstRefused(key, c.IP, s.now()) {
			s.loginDenied(ctx, c, u.ID, u.Pseudonym, "rate_limited", "locked")
		}
		return "", Principal{}, &LockedError{RetryAfter: locked}
	}
	// Attempts in parallel count before their result is known (T-002).
	tk, ok := s.throttle.reserve(key, c.IP, s.now(), lim)
	if !ok {
		s.reportFull(ctx, c)
		return "", Principal{}, &LockedError{RetryAfter: time.Second}
	}
	defer s.throttle.release(key, c.IP, tk)
	// Hanging up costs the rank of the line, or a client that never fails
	// could stay in front. Locks it causes are reported as ever.
	hungUp := func(err error) {
		if tk.place == nil || !errors.Is(err, context.Canceled) {
			return
		}
		pairLocked, clientLocked := s.throttle.fail(key, c.IP, s.now(), lim, tk)
		if pairLocked {
			s.lockedOut(ctx, c, u.ID, u.Pseudonym, "account", lim.lockout)
		}
		if clientLocked {
			s.lockedOut(ctx, c, u.ID, u.Pseudonym, "client", lim.lockout)
		}
	}
	if tk.place != nil {
		// The wait comes first: the delay counts what went before in line.
		d, turn, err := s.throttle.wait(ctx, tk.place, key, s.sleep, s.now, lim.lockout, lim.gap())
		// After the turn: the entry can take long, and the line goes on.
		s.reportFull(ctx, c)
		if err != nil {
			hungUp(err)
			return "", Principal{}, err
		}
		if !turn {
			return "", Principal{}, &LockedError{RetryAfter: time.Second}
		}
		delay = max(delay, d)
	}
	// A longer password cannot be right; it costs no more work than any other.
	if len(pw) > 4*password.MaxLength {
		pw = ""
	}
	if delay > 0 {
		if err := s.sleep(ctx, delay); err != nil {
			hungUp(err)
			return "", Principal{}, err
		}
	}
	fail := func(cause string) (string, Principal, error) {
		s.loginDenied(ctx, c, u.ID, u.Pseudonym, "unauthenticated", cause)
		pairLocked, clientLocked := s.throttle.fail(key, c.IP, s.now(), lim, tk)
		if pairLocked {
			s.lockedOut(ctx, c, u.ID, u.Pseudonym, "account", lim.lockout)
		}
		if clientLocked {
			s.lockedOut(ctx, c, u.ID, u.Pseudonym, "client", lim.lockout)
		}
		return "", Principal{}, ErrInvalidCredentials
	}

	if !known {
		if err := s.hasher.VerifyDummy(ctx, pw); err != nil {
			hungUp(err)
			return "", Principal{}, err
		}
		return fail("unknown_user")
	}

	match, err := s.hasher.Verify(ctx, u.PasswordHash, pw)
	if err != nil {
		hungUp(err)
		return "", Principal{}, err
	}
	reason := ""
	switch {
	case !match:
		reason = "wrong_password"
	case u.DisabledAt.Valid:
		reason = "disabled"
	case u.IsEmergency == 1 && !s.emergency:
		reason = "emergency_admin_disabled"
	}
	if reason != "" {
		return fail(reason)
	}

	// With the second factor on, the password opens only the second step;
	// the failures stay counted until the code is right (AUTH-008).
	if u.TotpEnabled && s.sf != nil {
		pending, expires, err := s.startSecondStep(ctx, u, c)
		if err != nil {
			return "", Principal{}, err
		}
		return "", Principal{}, &SecondStepError{Token: pending, ExpiresAt: expires}
	}
	token, p, err := s.createSession(ctx, u, c)
	if err != nil {
		return "", Principal{}, err
	}
	s.throttle.success(key, c.IP, s.now(), lim.lockout)
	s.log.Info("login", logging.Event(logging.AuthLoginOK),
		slog.String(logging.KeyRequestID, c.RequestID), slog.String(logging.KeySession, p.SessionRef()),
		slog.String(logging.KeyUser, p.Pseudonym), slog.String(logging.KeyRole, p.Role))
	return token, p, nil
}

func (s *Service) createSession(ctx context.Context, u sqlitedb.GetLoginUserRow, c Client) (string, Principal, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", Principal{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := HashToken(token)
	idle, maxLife := s.policy(ctx)
	now := s.now().UTC()
	absolute := now.Add(maxLife)
	ua := c.UserAgent
	if len(ua) > maxUserAgent {
		ua = ua[:maxUserAgent]
	}

	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", Principal{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	if err := q.InsertSession(ctx, sqlitedb.InsertSessionParams{
		IDHash: hash, UserID: u.ID, CreatedAt: now.Format(store.TimeLayout), LastSeenAt: now.Format(store.TimeLayout),
		ExpiresAt: absolute.Format(store.TimeLayout), Ip: c.IP.String(), UserAgent: ua,
	}); err != nil {
		return "", Principal{}, fmt.Errorf("create session: %w", err)
	}
	if err := q.SetLastLogin(ctx, sqlitedb.SetLastLoginParams{LastLoginAt: sql.NullString{String: now.Format(store.TimeLayout), Valid: true}, ID: u.ID}); err != nil {
		return "", Principal{}, err
	}
	if _, err := audit.Write(ctx, tx, audit.Entry{
		Kind: audit.KindAuth, ActorID: u.ID, ActorRole: u.Role, SessionID: hash,
		TargetType: "user", TargetID: u.ID, Action: "login", Result: audit.ResultOK,
		Payload: map[string]string{"ip": c.IP.String(), "request_id": c.RequestID},
	}); err != nil {
		return "", Principal{}, err
	}
	// The session the browser sent along ends with the login when it is the
	// user's own; a session of another user stays (AUTH-006, D-052).
	var ended []string
	if c.SessionToken != "" {
		if ended, err = q.DeleteSessionOfUser(ctx, sqlitedb.DeleteSessionOfUserParams{
			IDHash: HashToken(c.SessionToken), UserID: u.ID,
		}); err != nil {
			return "", Principal{}, fmt.Errorf("end previous session: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return "", Principal{}, fmt.Errorf("create session: %w", err)
	}
	for _, h := range ended {
		s.log.Info("session ended", logging.Event(logging.AuthSessionRevoked),
			slog.String(logging.KeyRequestID, c.RequestID), slog.String(logging.KeySession, h[:8]),
			slog.String("user_id", u.ID), slog.String("cause", "login"))
	}
	s.sessionsEnded(ended...)
	return token, Principal{
		UserID: u.ID, Pseudonym: u.Pseudonym, Role: u.Role, IsEmergency: u.IsEmergency == 1,
		MustChangePassword: u.MustChangePassword == 1,
		TOTPSetupRequired:  !u.TotpEnabled && s.sf != nil && s.TOTPRequired(ctx),
		SessionHash:        hash, ExpiresAt: minTime(now.Add(idle), absolute),
	}, nil
}

// loginDenied logs and audits a refused login with the API reason code and
// the internal cause. The attempted pseudonym is recorded only for existing
// users, so a password typed into the name field never lands in log or
// audit.
func (s *Service) loginDenied(ctx context.Context, c Client, userID, pseudonym, reason, cause string) {
	attrs := []any{
		logging.Event(logging.AuthLoginFailed), slog.String(logging.KeyRequestID, c.RequestID),
		slog.String(logging.KeyReason, reason), slog.String("cause", cause),
	}
	if pseudonym != "" {
		attrs = append(attrs, slog.String(logging.KeyUser, pseudonym))
	}
	s.log.Info("login failed", attrs...)
	s.audit(ctx, audit.Entry{
		Kind: audit.KindAuth, TargetType: "user", TargetID: userID, Action: "login",
		Result: audit.ResultDenied, Reason: reason,
		Payload: map[string]string{"ip": c.IP.String(), "cause": cause, "request_id": c.RequestID},
	})
}

func (s *Service) reportFull(ctx context.Context, c Client) {
	if s.throttle.reportFull(s.now()) {
		s.lockedOut(ctx, c, "", "", "table", fullReportGap)
	}
}

// lockedOut records a lockout that began: of one account for this client, of
// this client for all names, or the start of the overflow line of a full table.
func (s *Service) lockedOut(ctx context.Context, c Client, userID, pseudonym, scope string, lockout time.Duration) {
	attrs := []any{
		logging.Event(logging.AuthLockout), slog.String(logging.KeyRequestID, c.RequestID),
		slog.String(logging.KeyReason, "rate_limited"), slog.String("ip", c.IP.String()),
		slog.String("scope", scope), slog.Duration("lockout", lockout),
	}
	if pseudonym != "" {
		attrs = append(attrs, slog.String(logging.KeyUser, pseudonym))
	}
	s.log.Warn("login locked for this client", attrs...)
	s.audit(ctx, audit.Entry{
		Kind: audit.KindAuth, TargetType: "user", TargetID: userID, Action: "lockout",
		Result: audit.ResultDenied, Reason: "rate_limited",
		Payload: map[string]string{"ip": c.IP.String(), "scope": scope, "duration": lockout.String(), "request_id": c.RequestID},
	})
}

// audit writes an entry outside a transaction; a failure is logged, the
// login answer stays the same.
func (s *Service) audit(ctx context.Context, e audit.Entry) {
	// A client that hangs up must not keep its attempt out of the log.
	if _, err := audit.Write(context.WithoutCancel(ctx), s.st.DB, e); err != nil {
		s.log.Error("audit entry not written", slog.String("action", e.Action), slog.Any("err", err))
	}
}

// Authenticate resolves a session token. Expired sessions and sessions of
// users that are disabled, deleted or a disabled emergency admin are
// removed and rejected.
func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	if token == "" {
		return Principal{}, ErrUnauthenticated
	}
	hash := HashToken(token)
	row, err := s.st.Queries.GetSessionUser(ctx, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, fmt.Errorf("look up session: %w", err)
	}
	idle, _ := s.policy(ctx)
	now := s.now().UTC()
	lastSeen, err1 := time.Parse(store.TimeLayout, row.LastSeenAt)
	absolute, err2 := time.Parse(store.TimeLayout, row.ExpiresAt)
	valid := err1 == nil && err2 == nil && now.Before(absolute) && now.Before(lastSeen.Add(idle)) &&
		!row.DisabledAt.Valid && !row.DeletedAt.Valid && (row.IsEmergency == 0 || s.emergency)
	if !valid {
		if _, err := s.st.Queries.DeleteSession(ctx, hash); err != nil {
			return Principal{}, fmt.Errorf("remove session: %w", err)
		}
		s.sessionsEnded(hash)
		return Principal{}, ErrUnauthenticated
	}
	if now.Sub(lastSeen) >= touchInterval {
		if err := s.st.Queries.TouchSession(ctx, sqlitedb.TouchSessionParams{LastSeenAt: now.Format(store.TimeLayout), IDHash: hash}); err != nil {
			return Principal{}, fmt.Errorf("touch session: %w", err)
		}
		lastSeen = now
	}
	return Principal{
		UserID: row.UserID, Pseudonym: row.Pseudonym, Role: row.Role, IsEmergency: row.IsEmergency == 1,
		MustChangePassword: row.MustChangePassword == 1,
		TOTPSetupRequired:  !row.TotpEnabled && s.sf != nil && s.TOTPRequired(ctx),
		SessionHash:        hash, ExpiresAt: minTime(lastSeen.Add(idle), absolute),
	}, nil
}

// Logout ends the session of p.
func (s *Service) Logout(ctx context.Context, p Principal, requestID string) error {
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := sqlitedb.New(tx).DeleteSession(ctx, p.SessionHash); err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	if _, err := audit.Write(ctx, tx, audit.Entry{
		Kind: audit.KindAuth, ActorID: p.UserID, ActorRole: p.Role, SessionID: p.SessionHash,
		TargetType: "user", TargetID: p.UserID, Action: "logout", Result: audit.ResultOK,
		Payload: map[string]string{"request_id": requestID},
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	s.log.Info("logout", logging.Event(logging.AuthLogout), slog.String(logging.KeyRequestID, requestID),
		slog.String(logging.KeySession, p.SessionRef()), slog.String(logging.KeyUser, p.Pseudonym),
		slog.String(logging.KeyRole, p.Role))
	s.sessionsEnded(p.SessionHash)
	return nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// RevokeUser ends all sessions of a user, for example after a lock,
// deletion or change of rights (AUTH-006, decision W2). reason names the
// cause in the log.
func (s *Service) RevokeUser(ctx context.Context, userID, reason, requestID string) error {
	hashes, err := s.st.Queries.DeleteUserSessions(ctx, userID)
	if err != nil {
		return fmt.Errorf("end sessions: %w", err)
	}
	for _, h := range hashes {
		s.log.Info("session ended", logging.Event(logging.AuthSessionRevoked),
			slog.String(logging.KeyRequestID, requestID), slog.String(logging.KeySession, h[:8]),
			slog.String("user_id", userID), slog.String("cause", reason))
	}
	s.sessionsEnded(hashes...)
	return nil
}

// RevokeOtherSessions ends all sessions of a user except keep, for example
// after a password change.
func (s *Service) RevokeOtherSessions(ctx context.Context, userID, keepHash, reason, requestID string) error {
	hashes, err := s.st.Queries.DeleteOtherSessions(ctx, sqlitedb.DeleteOtherSessionsParams{UserID: userID, IDHash: keepHash})
	if err != nil {
		return fmt.Errorf("end sessions: %w", err)
	}
	for _, h := range hashes {
		s.log.Info("session ended", logging.Event(logging.AuthSessionRevoked),
			slog.String(logging.KeyRequestID, requestID), slog.String(logging.KeySession, h[:8]),
			slog.String("user_id", userID), slog.String("cause", reason))
	}
	s.sessionsEnded(hashes...)
	return nil
}

// SessionValid reports whether a session still exists and may be used: not
// past its maximum lifetime and its user neither disabled nor deleted. Open
// WebSockets check it regularly (AUTH-006).
func (s *Service) SessionValid(ctx context.Context, hash string) bool {
	row, err := s.st.Queries.GetSessionUser(ctx, hash)
	if err != nil {
		return false
	}
	absolute, err := time.Parse(store.TimeLayout, row.ExpiresAt)
	return err == nil && s.now().UTC().Before(absolute) && !row.DisabledAt.Valid && !row.DeletedAt.Valid &&
		(row.IsEmergency == 0 || s.emergency) && row.MustChangePassword == 0 &&
		(row.TotpEnabled || s.sf == nil || !s.TOTPRequired(ctx))
}
