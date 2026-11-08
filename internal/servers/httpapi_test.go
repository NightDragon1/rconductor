// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"bytes"
	"context"
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
	m := connmgr.New(rcon.Registry{httpapi.Protocol: httpapi.Adapter{}}, f.svc, log, connmgr.Options{ProbeInterval: 50 * time.Millisecond, MaxBackoff: 100 * time.Millisecond})
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
		in := ConnectionData{ProfileID: &profile, Host: &host, Port: &port, Password: secretOf(apiToken)}

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
		in := Input{Name: &name, ProfileID: &profile, Host: &host, Port: &port, Password: secretOf(apiToken)}
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
