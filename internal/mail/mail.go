// SPDX-License-Identifier: AGPL-3.0-or-later

// Package mail sends the panel's mail over the SMTP server the RCON admin
// sets up (NFR-017): host, port, encryption (none, STARTTLS, SSL), user,
// password and sender. The password is sealed with the master key like
// RCON credentials (SRV-002) and never leaves the package; with STARTTLS
// and SSL the certificate is always checked, and STARTTLS never falls back
// to plain text. Logs name only the recipient's domain and the reason as a
// category.
package mail

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/secret"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Encryption of the SMTP connection.
const (
	EncryptionNone     = "none"
	EncryptionSTARTTLS = "starttls"
	EncryptionSSL      = "ssl"
)

// Categories of a failed mail (NFR-017): the only reason a log line, the
// audit or the API names.
const (
	CategoryAuth     = "auth"
	CategoryTLS      = "tls"
	CategoryConnect  = "connect"
	CategoryTimeout  = "timeout"
	CategoryRejected = "rejected"
)

// Settings of the mail (Design 5.6); they live in users with the other
// global settings.
const (
	SettingTimeout       = users.SettingSMTPTimeout
	DefaultTimeout       = users.DefaultSMTPTimeout
	MinTimeout           = users.MinSMTPTimeout
	SettingTestPerMinute = users.SettingSMTPTestPerMinute
	DefaultTestPerMinute = users.DefaultSMTPTestPerMinute
	MinTestPerMinute     = users.MinSMTPTestPerMinute
)

// Field lengths.
const (
	maxHost     = 253
	maxUser     = 256
	maxPassword = 512
	maxAddress  = 254
	maxTesters  = 1000
)

// The test mail is fixed: no input of a user goes into it but the
// recipient (NFR-017).
const (
	testSubject = "RCONductor test mail"
	testBody    = "This is a test mail from RCONductor. The panel can send mail over the configured SMTP server.\r\n"
)

// ErrRateLimited means the caller sent too many test mails this minute.
var ErrRateLimited = errors.New("too many test mails, wait a minute")

// View is the SMTP setup as the API shows it: the password only as set or
// not.
type View struct {
	Host        string
	Port        int
	Encryption  string
	Username    string
	PasswordSet bool
	From        string
	Version     int64
}

// Input holds the changes of a PUT; nil fields stay. An empty Password
// removes it; a new host or port keeps the stored one (D-050).
type Input struct {
	Host       *string
	Port       *int
	Encryption *string
	Username   *string
	Password   *logging.Secret
	From       *string
}

// Result of a test mail: ok, or the category of the failure.
type Result struct {
	OK       bool
	Category string
}

// Service is the SMTP setup and the sending of mail.
type Service struct {
	st  *store.Store
	box *secret.Box
	log *slog.Logger
	now func() time.Time
	// roots are the trusted roots for the SMTP certificate; nil takes the
	// system's. Only tests set others.
	roots *x509.CertPool
	// timeout overrides smtp.timeout below its minimum, for tests only.
	timeout time.Duration

	mu      sync.Mutex
	testers map[string]*window
	// overflow is the one window that all new users share while testers
	// is full; it has the limit of one user (D-077).
	overflow window
}

type window struct {
	start time.Time
	count int
}

// NewService returns the mail service; box seals the password with the
// current master key.
func NewService(st *store.Store, box *secret.Box, log *slog.Logger) *Service {
	return &Service{st: st, box: box, log: log, now: time.Now, testers: map[string]*window{}}
}

// NewTestService trusts roots for the SMTP certificate and waits at most
// timeout, for tests only; the check of the certificate itself stays.
func NewTestService(st *store.Store, box *secret.Box, log *slog.Logger, roots *x509.CertPool, timeout time.Duration) *Service {
	s := NewService(st, box, log)
	s.roots, s.timeout = roots, timeout
	return s
}

// EnsureDefaults stores the defaults of the settings unless they exist.
func EnsureDefaults(ctx context.Context, st *store.Store) error {
	if _, err := st.EnsureDefault(ctx, SettingTimeout, DefaultTimeout.String()); err != nil {
		return err
	}
	_, err := st.EnsureDefault(ctx, SettingTestPerMinute, DefaultTestPerMinute)
	return err
}

func secretContext() secret.Context {
	return secret.Context{Table: "smtp_settings", ID: "1", Column: "password"}
}

func (s *Service) denied(ctx context.Context, req users.Request, action string) error {
	perm.LogDenied(s.log, perm.Denial{
		RequestID: req.RequestID, SessionRef: req.SessionRef, User: req.Pseudonym,
		Role: req.Actor.Role, Reason: "forbidden", Action: action,
	})
	if _, err := audit.Write(ctx, s.st.DB, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		TargetType: "settings", Action: action, Result: audit.ResultDenied, Reason: "forbidden",
	}); err != nil {
		s.log.Error("audit entry not written", slog.String("action", action), slog.Any("err", err))
	}
	return users.ErrForbidden
}

// Get returns the SMTP setup; RCON admin only.
func (s *Service) Get(ctx context.Context, req users.Request) (View, error) {
	if req.Actor.Role != perm.RCONAdmin {
		return View{}, s.denied(ctx, req, "settings.smtp.read")
	}
	r, err := s.st.Queries.GetSMTPSettings(ctx)
	if err != nil {
		return View{}, err
	}
	return toView(r), nil
}

func toView(r sqlitedb.GetSMTPSettingsRow) View {
	return View{Host: r.Host, Port: int(r.Port), Encryption: r.Encryption, Username: r.Username,
		PasswordSet: len(r.Password) > 0, From: r.Sender, Version: r.Version}
}

// hasControl reports control or format characters, among them CR and LF
// that would start another header line.
func hasControl(v string) bool {
	return strings.IndexFunc(v, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) >= 0
}

// address checks a mail address: one plain address, no name, no control
// characters (no header injection).
func address(v string) (string, error) {
	if v == "" || len(v) > maxAddress || hasControl(v) {
		return "", errors.New("not a mail address")
	}
	a, err := mail.ParseAddress(v)
	if err != nil || a.Name != "" || a.Address != v {
		return "", errors.New("not a plain mail address")
	}
	return a.Address, nil
}

// validHost accepts a host name or an IP address, nothing with spaces,
// slashes, user parts or ports.
func validHost(h string) bool {
	if h == "" || len(h) > maxHost || hasControl(h) {
		return false
	}
	if net.ParseIP(h) != nil {
		return true
	}
	return !strings.ContainsAny(h, " /@:[]")
}

// Update changes the SMTP setup as a whole with If-Match; RCON admin only.
// User and password are allowed only with STARTTLS or SSL. The change is
// audited without the password (NFR-017).
func (s *Service) Update(ctx context.Context, req users.Request, in Input, version *int64) (View, error) {
	if req.Actor.Role != perm.RCONAdmin {
		return View{}, s.denied(ctx, req, "settings.smtp.update")
	}
	if version == nil {
		return View{}, users.ErrPreconditionRequired
	}
	cur, err := s.st.Queries.GetSMTPSettings(ctx)
	if err != nil {
		return View{}, err
	}
	next := cur
	if in.Host != nil {
		if !validHost(*in.Host) {
			return View{}, &users.InvalidError{Detail: "host must be a host name or an IP address"}
		}
		next.Host = *in.Host
	}
	if in.Port != nil {
		if *in.Port < 1 || *in.Port > 65535 {
			return View{}, &users.InvalidError{Detail: "port must be from 1 to 65535"}
		}
		next.Port = int64(*in.Port)
	}
	if in.Encryption != nil {
		switch *in.Encryption {
		case EncryptionNone, EncryptionSTARTTLS, EncryptionSSL:
			next.Encryption = *in.Encryption
		default:
			return View{}, &users.InvalidError{Detail: "encryption must be none, starttls or ssl"}
		}
	}
	if in.Username != nil {
		if len(*in.Username) > maxUser || hasControl(*in.Username) {
			return View{}, &users.InvalidError{Detail: "user too long or with control characters"}
		}
		next.Username = *in.Username
	}
	passwordChanged := false
	if in.Password != nil {
		switch {
		case *in.Password == "":
			next.Password = nil
		case len(*in.Password) > maxPassword:
			return View{}, &users.InvalidError{Detail: "password too long"}
		default:
			sealed, err := s.box.Seal(*in.Password, secretContext())
			if err != nil {
				return View{}, err
			}
			next.Password = sealed
		}
		passwordChanged = true
	}
	if in.From != nil {
		if *in.From != "" {
			a, err := address(*in.From)
			if err != nil {
				return View{}, &users.InvalidError{Detail: "sender must be a plain mail address"}
			}
			*in.From = a
		}
		next.Sender = *in.From
	}
	if next.Encryption == EncryptionNone && (next.Username != "" || len(next.Password) > 0) {
		return View{}, &users.InvalidError{Detail: "user and password need starttls or ssl"}
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return View{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	n, err := q.UpdateSMTPSettings(ctx, sqlitedb.UpdateSMTPSettingsParams{
		Host: next.Host, Port: next.Port, Encryption: next.Encryption, Username: next.Username,
		Password: next.Password, Sender: next.Sender, UpdatedAt: store.Now(), Version: *version,
	})
	if err != nil {
		return View{}, err
	}
	if n == 0 {
		return View{}, users.ErrConflict
	}
	changes := map[string]any{}
	for k, pair := range map[string][2]string{
		"host": {cur.Host, next.Host}, "port": {strconv.FormatInt(cur.Port, 10), strconv.FormatInt(next.Port, 10)},
		"encryption": {cur.Encryption, next.Encryption}, "username": {cur.Username, next.Username},
		"from": {cur.Sender, next.Sender},
	} {
		if pair[0] != pair[1] {
			changes[k] = map[string]string{"from": pair[0], "to": pair[1]}
		}
	}
	if passwordChanged {
		changes["password"] = map[string]bool{"set": len(next.Password) > 0}
	}
	if _, err := audit.Write(ctx, tx, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		TargetType: "settings", Action: "settings.smtp.update", Payload: changes, Result: audit.ResultOK,
	}); err != nil {
		return View{}, err
	}
	if err := tx.Commit(); err != nil {
		return View{}, err
	}
	return s.Get(ctx, req)
}

func (s *Service) duration(ctx context.Context) time.Duration {
	if s.timeout > 0 {
		return s.timeout
	}
	var v string
	if err := s.st.GlobalSetting(ctx, SettingTimeout, &v); err == nil {
		if d, err := time.ParseDuration(v); err == nil {
			return max(d, MinTimeout)
		}
	}
	return DefaultTimeout
}

func (s *Service) take(ctx context.Context, user string) bool {
	limit := DefaultTestPerMinute
	var v int
	if err := s.st.GlobalSetting(ctx, SettingTestPerMinute, &v); err == nil {
		limit = max(v, MinTestPerMinute)
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.testers[user]
	if w == nil {
		// A full table drops only windows whose minute ended; if it stays
		// full, new users share the overflow window, and no count already
		// held is lost (D-076, D-077).
		if len(s.testers) >= maxTesters {
			for k, old := range s.testers {
				if now.Sub(old.start) >= time.Minute {
					delete(s.testers, k)
				}
			}
		}
		if len(s.testers) >= maxTesters {
			w = &s.overflow
		} else {
			w = &window{start: now}
			s.testers[user] = w
		}
	}
	if now.Sub(w.start) >= time.Minute {
		*w = window{start: now}
	}
	if w.count >= limit {
		return false
	}
	w.count++
	return true
}

func domainOf(addr string) string {
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		return strings.ToLower(addr[i+1:])
	}
	return ""
}

// Test sends the fixed test mail to the recipient; RCON admin only, at
// most smtp.test_per_minute per user and minute. The answer and the log
// name only the category of a failure (NFR-017).
func (s *Service) Test(ctx context.Context, req users.Request, to string) (Result, error) {
	if req.Actor.Role != perm.RCONAdmin {
		return Result{}, s.denied(ctx, req, "settings.smtp.test")
	}
	rcpt, err := address(to)
	if err != nil {
		return Result{}, &users.InvalidError{Detail: "recipient must be a plain mail address"}
	}
	if !s.take(ctx, req.Actor.ID) {
		return Result{}, ErrRateLimited
	}
	r, err := s.st.Queries.GetSMTPSettings(ctx)
	if err != nil {
		return Result{}, err
	}
	if r.Host == "" || r.Sender == "" {
		return Result{}, &users.InvalidError{Detail: "set host and sender first"}
	}
	category := s.send(ctx, r, rcpt, testSubject, testBody)
	res := Result{OK: category == "", Category: category}
	attrs := []any{slog.String(logging.KeyRequestID, req.RequestID), slog.String("domain", domainOf(rcpt))}
	result, reason := audit.ResultOK, ""
	if res.OK {
		s.log.Info("mail sent", append(attrs, logging.Event(logging.MailSent))...)
	} else {
		s.log.Warn("mail not sent", append(attrs, logging.Event(logging.MailFailed), slog.String(logging.KeyReason, category))...)
		result, reason = audit.ResultError, category
	}
	if _, err := audit.Write(ctx, s.st.DB, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		TargetType: "settings", Action: "settings.smtp.test", Payload: map[string]string{"domain": domainOf(rcpt)},
		Result: result, Reason: reason,
	}); err != nil {
		s.log.Error("audit entry not written", slog.String("action", "settings.smtp.test"), slog.Any("err", err))
	}
	return res, nil
}

// send delivers one mail and returns the category of a failure, or "".
func (s *Service) send(ctx context.Context, r sqlitedb.GetSMTPSettingsRow, to, subject, body string) string {
	var password logging.Secret
	if len(r.Password) > 0 {
		pw, err := s.box.Open(r.Password, secretContext())
		if err != nil {
			// Sealed with another master key: it must be entered again.
			return CategoryAuth
		}
		password = pw
	}
	timeout := s.duration(ctx)
	sctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	msg := message(r.Sender, to, subject, body, s.now())
	err := deliver(sctx, dialSpec{
		host: r.Host, port: int(r.Port), encryption: r.Encryption, user: r.Username, password: password,
		roots: s.roots, deadline: s.now().Add(timeout),
	}, r.Sender, to, msg)
	if err == nil {
		return ""
	}
	var f *failure
	if errors.As(err, &f) {
		return f.category
	}
	return CategoryConnect
}

func message(from, to, subject, body string, now time.Time) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", (&mail.Address{Address: from}).String())
	fmt.Fprintf(&b, "To: %s\r\n", (&mail.Address{Address: to}).String())
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "Date: %s\r\n", now.UTC().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(body)
	return []byte(b.String())
}

// The recovery mail of the second factor is fixed but for the link
// (AUTH-008); the link carries its token in the fragment and is never
// logged.
const (
	recoverySubject = "RCONductor: reset your two-factor sign-in"
	recoveryBody    = "Someone asked to reset the two-factor sign-in of your RCONductor account.\r\n\r\n" +
		"If it was you, open this link within the time it is valid and enter your password:\r\n\r\n%s\r\n\r\n" +
		"The link works once. If it was not you, ignore this mail; nothing changes.\r\n"
)

// SendRecovery sends the recovery mail of the second factor and returns
// the category of a failure, or "" (NFR-017, AUTH-008).
func (s *Service) SendRecovery(ctx context.Context, to, link string) string {
	attrs := []any{slog.String("domain", domainOf(to)), slog.String("purpose", "recovery")}
	r, err := s.st.Queries.GetSMTPSettings(ctx)
	if err != nil || r.Host == "" || r.Sender == "" {
		s.log.Warn("mail not sent", append(attrs, logging.Event(logging.MailFailed), slog.String(logging.KeyReason, CategoryConnect))...)
		return CategoryConnect
	}
	category := s.send(ctx, r, to, recoverySubject, fmt.Sprintf(recoveryBody, link))
	if category == "" {
		s.log.Info("mail sent", append(attrs, logging.Event(logging.MailSent))...)
	} else {
		s.log.Warn("mail not sent", append(attrs, logging.Event(logging.MailFailed), slog.String(logging.KeyReason, category))...)
	}
	return category
}
