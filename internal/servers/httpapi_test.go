// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/connmgr"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/rcon"
	"github.com/NightDragon1/rconductor/internal/rcon/httpapi"
	"github.com/NightDragon1/rconductor/internal/rcon/httpapi/httpapitest"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
)

const apiToken = "token-of-the-api"

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// httpFixture is a server service with the http-api adapter, a profile
// for each dialect and the fake game server of each dialect.
type httpFixture struct {
	svc  *Service
	f    *fixture
	logs *lockedBuffer
}

func newHTTPFixture(t *testing.T) *httpFixture {
	t.Helper()
	st := storetest.New(t)
	logs := &lockedBuffer{}
	log := logging.New(logs, "panel", config.Log{Format: "json"}).Module("servers")
	c := perm.NewChecker(st, log)
	f := &fixture{t: t, st: st, c: c, svc: NewService(st, c, newBox(t), log), ids: map[string]string{}}
	f.ids["admin"] = storetest.CreateUser(t, st, storetest.User{Pseudonym: "admin", Role: "rcon_admin"})
	f.ids["sa"] = storetest.CreateUser(t, st, storetest.User{Pseudonym: "sa", Role: "server_admin"})
	custom := t.TempDir()
	for _, d := range httpapi.Names() {
		y := "schema: 1\nid: test-" + d + "\nname: Test " + d + "\nprotocol: { type: http-api, default_port: 7777 }\nhttp_api: { dialect: " + d + " }\n"
		if err := os.WriteFile(filepath.Join(custom, "test-"+d+".yaml"), []byte(y), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := profiles.NewImporter(st, log, os.DirFS("../../profiles"), custom).Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := connmgr.New(rcon.Registry{httpapi.Protocol: httpapi.Adapter{}}, f.svc, log, connmgr.Options{ProbeInterval: 50 * time.Millisecond, MaxBackoff: 100 * time.Millisecond, OnCertificate: f.svc.CertificateSeen})
	f.svc.SetConnections(m)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Wait)
	t.Cleanup(cancel)
	return &httpFixture{svc: f.svc, f: f, logs: logs}
}

func (h *httpFixture) profile(t *testing.T, dialect string) string {
	t.Helper()
	var id string
	if err := h.f.st.DB.QueryRow(`SELECT id FROM game_profiles WHERE slug = ?`, "test-"+dialect).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func hostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, port, _ := net.SplitHostPort(addr)
	p, _ := strconv.Atoi(port)
	return host, p
}

// TestPinnedConnectionTest covers PROT-003 and D-071 for the connection
// test: without a pin it only reads the certificate, on a mismatch it
// sends nothing; with the right pin the token is checked. The fake server
// counts requests with a token.
func TestPinnedConnectionTest(t *testing.T) {
	h := newHTTPFixture(t)
	ctx := context.Background()
	for _, d := range httpapi.Names() {
		s := httpapitest.Start(httpapitest.Fixtures[d], apiToken, 0, func(string) string { return "ok" })
		t.Cleanup(s.Close)
		host, port := hostPort(t, s.Addr())
		profile := h.profile(t, d)
		in := ConnectionData{ProfileIDs: &[]string{profile}, Host: &host, Port: &port, Password: secretOf(apiToken)}

		got, err := h.svc.TestConnection(ctx, h.f.as("admin"), in)
		if err != nil || got.Result != ConnectionUnpinned || got.Fingerprint != s.Fingerprint {
			t.Fatalf("%s unpinned: %+v %v", d, got, err)
		}
		other := strings.Repeat("cd", 32)
		in.TLSFingerprint = &other
		if got, err := h.svc.TestConnection(ctx, h.f.as("admin"), in); err != nil || got.Result != ConnectionCertMismatch || got.Fingerprint != s.Fingerprint {
			t.Errorf("%s mismatch: %+v %v", d, got, err)
		}
		if n, a := s.Requests.Load(), s.Authorized.Load(); n != 0 || a != 0 {
			t.Fatalf("%s: %d requests, %d with the token before a matching pin", d, n, a)
		}
		in.TLSFingerprint = &s.Fingerprint
		if got, err := h.svc.TestConnection(ctx, h.f.as("admin"), in); err != nil || got.Result != ConnectionOK {
			t.Errorf("%s match: %+v %v", d, got, err)
		}
		in.Password = secretOf("wrong")
		if got, err := h.svc.TestConnection(ctx, h.f.as("admin"), in); err != nil || got.Result != ConnectionAuthFailed {
			t.Errorf("%s wrong token: %+v %v", d, got, err)
		}
		if strings.Contains(h.logs.String(), apiToken) {
			t.Error("the token is in the log")
		}
	}
}

// TestHTTPServerPinning covers PROT-003 for servers: a server with an
// HTTPS API needs a trusted certificate; it connects with it; another
// certificate gives cert_mismatch and sends nothing. Trusting logs
// server.cert.pinned, a mismatch server.cert.mismatch.
func TestHTTPServerPinning(t *testing.T) {
	h := newHTTPFixture(t)
	ctx := context.Background()
	for _, d := range httpapi.Names() {
		s := httpapitest.Start(httpapitest.Fixtures[d], apiToken, 0, func(string) string { return "ok" })
		t.Cleanup(s.Close)
		host, port := hostPort(t, s.Addr())
		profile := h.profile(t, d)
		name := "srv-" + d
		in := Input{Name: &name, ProfileIDs: &[]string{profile}, Host: &host, Port: &port, Password: secretOf(apiToken)}
		if _, err := h.svc.Create(ctx, h.f.as("admin"), in); err == nil {
			t.Fatalf("%s: created without a trusted certificate", d)
		}
		bad := "zz"
		in.TLSFingerprint = &bad
		if _, err := h.svc.Create(ctx, h.f.as("admin"), in); err == nil {
			t.Fatalf("%s: created with a bad fingerprint", d)
		}
		upper := strings.ToUpper(s.Fingerprint)
		in.TLSFingerprint = &upper
		sv, err := h.svc.Create(ctx, h.f.as("admin"), in)
		if err != nil {
			t.Fatal(err)
		}
		if sv.TLSFingerprint != s.Fingerprint {
			t.Errorf("stored fingerprint %q", sv.TLSFingerprint)
		}
		waitStatus(t, h.svc, sv.ID, connmgr.Connected)

		other := strings.Repeat("ef", 32)
		v := version(t, h.f, sv.ID)
		if _, err := h.svc.Update(ctx, h.f.as("admin"), sv.ID, Input{TLSFingerprint: &other, Version: &v}); err != nil {
			t.Fatal(err)
		}
		waitStatus(t, h.svc, sv.ID, connmgr.CertMismatch)
		// The worker keeps trying; a retry fails in the handshake, before
		// any request.
		before := s.Requests.Load()
		m := h.svc.conns.(*connmgr.Manager)
		if err := m.Reload(ctx, sv.ID); err != nil {
			t.Fatal(err)
		}
		waitStatus(t, h.svc, sv.ID, connmgr.CertMismatch)
		if s.Requests.Load() != before {
			t.Errorf("%s: requests after the mismatch", d)
		}
		// The server admin may not trust a certificate.
		v = version(t, h.f, sv.ID)
		if _, err := h.svc.Update(ctx, h.f.as("sa"), sv.ID, Input{TLSFingerprint: &s.Fingerprint, Version: &v}); err == nil {
			t.Error("a server admin changed the trusted certificate")
		}
	}
	logs := h.logs.String()
	for _, code := range []logging.Code{logging.ServerCertPinned, logging.ServerCertMismatch} {
		if !strings.Contains(logs, `"event":"`+string(code)+`"`) {
			t.Errorf("no %s in the log", code)
		}
	}
	if strings.Contains(logs, apiToken) {
		t.Error("the token is in the log")
	}
}

func waitStatus(t *testing.T, s *Service, id string, want connmgr.Status) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if st, _ := s.conns.Status(id); st == want {
			return
		}
		if time.Now().After(deadline) {
			st, _ := s.conns.Status(id)
			t.Fatalf("status %s, want %s", st, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func version(t *testing.T, f *fixture, id string) int64 {
	t.Helper()
	var v int64
	if err := f.st.DB.QueryRow(`SELECT version FROM servers WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// notAfter reads tls_not_after of a server straight from the table.
func (h *httpFixture) notAfter(t *testing.T, id string) (string, bool) {
	t.Helper()
	var v sql.NullString
	if err := h.f.st.DB.QueryRow(`SELECT tls_not_after FROM servers WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v.String, v.Valid
}

// TestCertificateNotAfter covers D-072 and D-073: saving a pin reads the
// certificate in a handshake without a request and stores its end of
// validity only if it is the pinned one; a new pin clears it; the first
// connection with the pin fills it in; the connection test and the
// server give it.
func TestCertificateNotAfter(t *testing.T) {
	h := newHTTPFixture(t)
	ctx := context.Background()
	for _, d := range httpapi.Names() {
		s := httpapitest.Start(httpapitest.Fixtures[d], apiToken, 0, func(string) string { return "ok" })
		t.Cleanup(s.Close)
		want := s.Certificate().NotAfter.UTC().Truncate(time.Second)
		host, port := hostPort(t, s.Addr())
		profile := h.profile(t, d)

		// The connection test gives the end of validity with the
		// fingerprint.
		res, err := h.svc.TestConnection(ctx, h.f.as("admin"), ConnectionData{ProfileIDs: &[]string{profile}, Host: &host, Port: &port, Password: secretOf(apiToken)})
		if err != nil || res.Fingerprint != s.Fingerprint || !res.NotAfter.Truncate(time.Second).Equal(want) {
			t.Fatalf("%s connection test: %+v %v", d, res, err)
		}

		// The handshake when saving sends no request, so no token.
		f := fields{protocol: httpapi.Protocol, host: host, port: port, fingerprint: s.Fingerprint}
		before, beforeAuth := s.Requests.Load(), s.Authorized.Load()
		h.svc.readNotAfter(ctx, "no-such-server", f)
		if s.Requests.Load() != before || s.Authorized.Load() != beforeAuth {
			t.Errorf("%s: the handshake when saving sent a request", d)
		}

		name := "na-" + d
		sv, err := h.svc.Create(ctx, h.f.as("admin"), Input{Name: &name, ProfileIDs: &[]string{profile}, Host: &host, Port: &port, Password: secretOf(apiToken), TLSFingerprint: &s.Fingerprint})
		if err != nil {
			t.Fatal(err)
		}
		if sv.TLSNotAfter == nil || !sv.TLSNotAfter.Equal(want) {
			t.Errorf("%s after create: %v, want %v", d, sv.TLSNotAfter, want)
		}

		// A pin the server does not show: unknown, not the old date.
		other := strings.Repeat("ef", 32)
		v := version(t, h.f, sv.ID)
		up, err := h.svc.Update(ctx, h.f.as("admin"), sv.ID, Input{TLSFingerprint: &other, Version: &v})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := h.notAfter(t, sv.ID); ok || up.TLSNotAfter != nil {
			t.Errorf("%s: other fingerprint kept an end of validity", d)
		}
		waitStatus(t, h.svc, sv.ID, connmgr.CertMismatch)
		if _, ok := h.notAfter(t, sv.ID); ok {
			t.Errorf("%s: a mismatching connection filled it in", d)
		}

		// Back to the shown certificate; then cleared by hand, the first
		// connection with the pin fills it in.
		v = version(t, h.f, sv.ID)
		if _, err := h.svc.Update(ctx, h.f.as("admin"), sv.ID, Input{TLSFingerprint: &s.Fingerprint, Version: &v}); err != nil {
			t.Fatal(err)
		}
		if got, ok := h.notAfter(t, sv.ID); !ok || got != want.Format(time.RFC3339) {
			t.Errorf("%s after the pin again: %q %v", d, got, ok)
		}
		if _, err := h.f.st.DB.Exec(`UPDATE servers SET tls_not_after = NULL WHERE id = ?`, sv.ID); err != nil {
			t.Fatal(err)
		}
		m := h.svc.conns.(*connmgr.Manager)
		if err := m.Reload(ctx, sv.ID); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			if got, ok := h.notAfter(t, sv.ID); ok {
				if got != want.Format(time.RFC3339) {
					t.Errorf("%s filled in: %q, want %s", d, got, want.Format(time.RFC3339))
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: not filled in at the connection", d)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// TestCertificateNotAfterExpired covers D-072: a server pinned to an
// expired certificate connects; the end of validity in the past is
// stored and given.
func TestCertificateNotAfterExpired(t *testing.T) {
	h := newHTTPFixture(t)
	ctx := context.Background()
	cert, err := httpapitest.ExpiredCertificate()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range httpapi.Names() {
		s := httpapitest.StartWithCertificate(httpapitest.Fixtures[d], cert, apiToken, 0, func(string) string { return "ok" })
		t.Cleanup(s.Close)
		host, port := hostPort(t, s.Addr())
		profile := h.profile(t, d)
		name := "expired-" + d
		sv, err := h.svc.Create(ctx, h.f.as("admin"), Input{Name: &name, ProfileIDs: &[]string{profile}, Host: &host, Port: &port, Password: secretOf(apiToken), TLSFingerprint: &s.Fingerprint})
		if err != nil {
			t.Fatal(err)
		}
		if sv.TLSNotAfter == nil || !sv.TLSNotAfter.Before(time.Now()) {
			t.Errorf("%s: end of validity %v, want one in the past", d, sv.TLSNotAfter)
		}
		waitStatus(t, h.svc, sv.ID, connmgr.Connected)
	}
}

// TestProfileListKeepsProtocolC11 covers D-076 and PROT-003: a profile list
// with another protocol is refused for every role and leaves protocol,
// port and pin alone; only the RCON admin changes the protocol, with new
// credentials in the same request, and the pin is dropped then. The audit
// names the protocol change (C12).
func TestProfileListKeepsProtocolC11(t *testing.T) {
	h := newHTTPFixture(t)
	ctx := context.Background()
	d := httpapi.Names()[0]
	s := httpapitest.Start(httpapitest.Fixtures[d], apiToken, 0, func(string) string { return "ok" })
	t.Cleanup(s.Close)
	host, port := hostPort(t, s.Addr())
	profile := h.profile(t, d)
	name := "pinned"
	sv, err := h.svc.Create(ctx, h.f.as("admin"), Input{Name: &name, ProfileIDs: &[]string{profile}, Host: &host, Port: &port, Password: secretOf(apiToken), TLSFingerprint: &s.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	g, err := h.svc.CreateGroup(ctx, h.f.as("admin"), "scope", []string{sv.ID})
	if err != nil {
		t.Fatal(err)
	}
	h.f.exec(`INSERT INTO admin_scopes (id, user_id, server_group_id, assigned_by, created_at) VALUES ('c11', ?, ?, ?, 't')`, h.f.ids["sa"], g.ID, h.f.ids["admin"])
	h.f.c.Invalidate()
	var rcon string
	if err := h.f.st.DB.QueryRow(`SELECT id FROM game_profiles WHERE slug = 'minecraft-java'`).Scan(&rcon); err != nil {
		t.Fatal(err)
	}
	unchanged := func(label string) {
		t.Helper()
		r, err := h.f.st.Queries.GetServer(ctx, sv.ID)
		if err != nil || r.Protocol != httpapi.Protocol || r.TlsFingerprint != s.Fingerprint || int(r.Port) != port {
			t.Errorf("%s: protocol %q pin %q port %d", label, r.Protocol, r.TlsFingerprint, r.Port)
		}
	}
	var inv *users.InvalidError
	if _, err := h.svc.Update(ctx, h.f.as("sa"), sv.ID, Input{ProfileIDs: &[]string{rcon}, Version: h.f.version(sv.ID)}); !errors.As(err, &inv) {
		t.Errorf("server admin, other protocol: %v, want 422", err)
	}
	unchanged("server admin")
	if _, err := h.svc.Update(ctx, h.f.as("admin"), sv.ID, Input{ProfileIDs: &[]string{rcon}, Version: h.f.version(sv.ID)}); !errors.As(err, &inv) {
		t.Errorf("RCON admin without credentials: %v, want 422", err)
	}
	unchanged("RCON admin without credentials")
	got, err := h.svc.Update(ctx, h.f.as("admin"), sv.ID, Input{ProfileIDs: &[]string{rcon}, Password: secretOf("a new rcon password"), Version: h.f.version(sv.ID)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Protocol != "source-rcon" || got.TLSFingerprint != "" || got.Port != port {
		t.Errorf("after the change: protocol %q pin %q port %d", got.Protocol, got.TLSFingerprint, got.Port)
	}
	var payload string
	if err := h.f.st.DB.QueryRow(`SELECT payload FROM audit_log WHERE action = 'server.update' AND target_id = ? ORDER BY rowid DESC LIMIT 1`, sv.ID).Scan(&payload); err != nil || !strings.Contains(payload, `"protocol"`) {
		t.Errorf("audit %q %v, want the protocol change", payload, err)
	}
}
