// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/connmgr"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/rcon"
	"github.com/NightDragon1/rconductor/internal/rcon/httpapi"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Results of a connection test (SRV-007).
const (
	ConnectionOK          = "ok"
	ConnectionAuthFailed  = "auth_failed"
	ConnectionUnreachable = "unreachable"
	// ConnectionUnpinned: no certificate of the server with an HTTPS API
	// is trusted yet; the test only read it (PROT-003, D-071).
	ConnectionUnpinned = "unpinned"
	// ConnectionCertMismatch: the server shows another certificate than
	// the trusted one; nothing was sent.
	ConnectionCertMismatch = "cert_mismatch"
)

// ConnectionResult is the result of a connection test; Fingerprint is the
// certificate a server with an HTTPS API showed.
type ConnectionResult struct {
	Result      string
	Fingerprint string
	// NotAfter is the end of validity of that certificate (D-073); zero
	// when no certificate was read.
	NotAfter time.Time
}

// ErrTestRateLimited means the user has used up his connection tests.
var ErrTestRateLimited = errors.New("too many connection tests")

// ConnectionData is what a connection test looks at; ServerID is empty for
// a server not created yet, nil fields keep the values of the server.
type ConnectionData struct {
	ServerID string
	// ProfileIDs are the profiles to test with, top first (SRV-008).
	ProfileIDs *[]string
	Host       *string
	Port       *int
	Password   *logging.Secret
	// TLSFingerprint is the certificate to trust for the test of a server
	// with an HTTPS API; without it the stored one counts.
	TLSFingerprint *string
}

// maxTestUsers bounds the memory of the limit; only RCON admins get there.
const maxTestUsers = 1000

// testWindow counts the tests of a user in the current minute.
type testWindow struct {
	start time.Time
	count int
}

// testLimiter limits connection tests per user and minute (Design 5.6).
type testLimiter struct {
	mu    sync.Mutex
	users map[string]*testWindow
}

func (l *testLimiter) take(user string, limit int, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.users == nil || len(l.users) >= maxTestUsers {
		l.users = map[string]*testWindow{}
	}
	w := l.users[user]
	if w == nil || now.Sub(w.start) >= time.Minute {
		w = &testWindow{start: now}
		l.users[user] = w
	}
	if w.count >= limit {
		return false
	}
	w.count++
	return true
}

func (s *Service) testLimit(ctx context.Context) int {
	n := users.DefaultConnectionTests
	var v int
	if err := s.st.GlobalSetting(ctx, users.SettingConnectionTests, &v); err == nil && v > 0 {
		n = v
	}
	return n
}

// TestConnection tests RCON data without saving them (SRV-007, D-049).
// Unchanged data of a server get the status of its running connection, so
// that nothing connects a second time; new or changed data open a short
// connection that only authenticates. Without a password typed in, the
// stored one is used, also at a new address (D-050). The password
// appears in neither log nor audit.
func (s *Service) TestConnection(ctx context.Context, req users.Request, in ConnectionData) (ConnectionResult, error) {
	const action = "server.connection_test"
	if err := s.mustBeAdmin(ctx, req, action, "server", in.ServerID); err != nil {
		return ConnectionResult{}, err
	}
	if !s.tests.take(req.Actor.ID, s.testLimit(ctx), time.Now()) {
		s.denied(ctx, req, action, "server", in.ServerID, "rate_limited")
		return ConnectionResult{}, ErrTestRateLimited
	}
	var base fields
	var sealed []byte
	if in.ServerID != "" {
		r, err := s.st.Queries.GetServer(ctx, in.ServerID)
		if errors.Is(err, sql.ErrNoRows) {
			return ConnectionResult{}, users.ErrNotFound
		}
		if err != nil {
			return ConnectionResult{}, err
		}
		base = fields{profileID: r.ProfileID.String, protocol: r.Protocol, host: r.Host, port: int(r.Port), fingerprint: r.TlsFingerprint}
		sealed = r.Secret
	}
	f, err := s.check(ctx, s.st.Queries, base, Input{ProfileIDs: in.ProfileIDs, Host: in.Host, Port: in.Port})
	if err != nil {
		return ConnectionResult{}, err
	}
	if f.host == "" || f.port == 0 {
		return ConnectionResult{}, &users.InvalidError{Detail: "profile and host are required"}
	}
	sameAddr := in.ServerID != "" && f.host == base.host && f.port == base.port
	var stored logging.Secret
	if in.ServerID != "" {
		stored = s.storedPassword(in.ServerID, sealed)
	}
	password := stored
	if in.Password != nil {
		if err := checkPassword(*in.Password); err != nil {
			return ConnectionResult{}, err
		}
		password = *in.Password
	}
	var res ConnectionResult
	if f.protocol == httpapi.Protocol {
		pin := base.fingerprint
		if in.TLSFingerprint != nil {
			pin = ""
			if fp, ok := httpapi.NormalizeFingerprint(*in.TLSFingerprint); ok {
				pin = fp
			}
		}
		res = s.tryPinned(ctx, req, in.ServerID, f, pin, password)
	} else {
		if password == "" {
			return ConnectionResult{}, &users.InvalidError{Detail: "enter the RCON password"}
		}
		if sameAddr && f.protocol == base.protocol && samePassword(password, stored) {
			res.Result = s.runningResult(in.ServerID)
		} else {
			res.Result = s.tryConnection(ctx, f, password)
		}
	}
	result := res.Result
	s.log.Info("rcon data tested", logging.Event(logging.RCONConnectionTest), slog.String(logging.KeyRequestID, req.RequestID),
		slog.String(logging.KeyUser, req.Pseudonym), slog.String(logging.KeyServer, in.ServerID), slog.String("result", result))
	if _, err := audit.Write(ctx, s.st.DB, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		ServerID: in.ServerID, TargetType: "server", TargetID: in.ServerID, Action: action, Result: audit.ResultOK,
		Payload: map[string]any{"host": f.host, "port": f.port, "result": result, "fingerprint": res.Fingerprint},
	}); err != nil {
		return ConnectionResult{}, err
	}
	return res, nil
}

// storedPassword opens the password of the read that gave the address, so
// that the two belong together (B4). Sealed with another key or missing: as if
// there were none.
func (s *Service) storedPassword(id string, sealed []byte) logging.Secret {
	pw, err := s.box.Open(sealed, secretContext(id))
	if err != nil {
		return ""
	}
	return pw
}

func samePassword(a, b logging.Secret) bool {
	return b != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (s *Service) runningResult(id string) string {
	st, _ := s.conns.Status(id)
	switch st {
	case connmgr.Connected:
		return ConnectionOK
	case connmgr.AuthFailed:
		return ConnectionAuthFailed
	}
	return ConnectionUnreachable
}

// tryPinned tests a server with an HTTPS API. The certificate is read in
// a handshake that sends nothing; the token goes out only if it is the
// trusted one (D-071).
func (s *Service) tryPinned(ctx context.Context, req users.Request, id string, f fields, pin string, token logging.Secret) ConnectionResult {
	addr := net.JoinHostPort(f.host, strconv.Itoa(f.port))
	cert, _, err := s.conns.Certificate(ctx, f.protocol, addr)
	if err != nil {
		s.log.Debug("certificate not readable", slog.Any("err", err))
		return ConnectionResult{Result: ConnectionUnreachable}
	}
	fp := cert.Fingerprint
	res := ConnectionResult{Fingerprint: fp, NotAfter: cert.NotAfter}
	switch {
	case pin == "":
		res.Result = ConnectionUnpinned
		return res
	case fp != pin:
		s.log.Warn("the game server shows another certificate than the trusted one", logging.Event(logging.ServerCertMismatch),
			slog.String(logging.KeyRequestID, req.RequestID), slog.String(logging.KeyServer, id), slog.String("fingerprint", fp))
		res.Result = ConnectionCertMismatch
		return res
	case token == "":
		res.Result = ConnectionAuthFailed
		return res
	}
	err = s.conns.Try(ctx, connmgr.Target{Protocol: f.protocol, Addr: addr, Password: token, Fingerprint: pin,
		Dialect: s.dialect(f.profileID), Parallel: 1})
	res.Result = connectionResult(err)
	if errors.Is(err, rcon.ErrCertMismatch) {
		res.Result = ConnectionCertMismatch
	}
	return res
}

func connectionResult(err error) string {
	switch {
	case err == nil:
		return ConnectionOK
	case errors.Is(err, rcon.ErrAuth):
		return ConnectionAuthFailed
	}
	return ConnectionUnreachable
}

func (s *Service) tryConnection(ctx context.Context, f fields, password logging.Secret) string {
	err := s.conns.Try(ctx, connmgr.Target{Protocol: f.protocol, Addr: net.JoinHostPort(f.host, strconv.Itoa(f.port)), Password: password})
	switch {
	case err == nil:
		return ConnectionOK
	case errors.Is(err, rcon.ErrAuth):
		return ConnectionAuthFailed
	}
	s.log.Debug("rcon data not reachable", slog.Any("err", err))
	return ConnectionUnreachable
}
