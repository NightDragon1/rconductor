// SPDX-License-Identifier: AGPL-3.0-or-later

package connmgr

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/rcon"
	"github.com/NightDragon1/rconductor/internal/rcon/httpapi"
	"github.com/NightDragon1/rconductor/internal/rcon/httpapi/httpapitest"
)

// swapServer is a fake http-api game server whose certificate can be
// replaced on the same address; it counts handshakes and every request
// that reaches the handler.
type swapServer struct {
	*httptest.Server
	cert       atomic.Pointer[tls.Certificate]
	requests   atomic.Int64
	handshakes atomic.Int64
}

func newCertificate(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "game"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, httpapi.Fingerprint(der)
}

// startSwapServer starts the server; without keepAlive every answer closes
// its connection, so that each request starts with a new handshake.
func startSwapServer(t *testing.T, token string, first tls.Certificate, keepAlive bool) *swapServer {
	t.Helper()
	s := &swapServer{}
	s.cert.Store(&first)
	inner := httpapitest.Fixtures["satisfactory"].Handler(token, 0, func(string) string { return "ok" })
	s.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		inner.ServeHTTP(w, r)
	}))
	s.TLS = &tls.Config{
		Certificates: []tls.Certificate{first},
		GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
			s.handshakes.Add(1)
			return &tls.Config{Certificates: []tls.Certificate{*s.cert.Load()}, NextProtos: []string{"http/1.1"}}, nil
		},
	}
	s.Config.SetKeepAlivesEnabled(keepAlive)
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}

// swap shows cert from the next handshake on and drops the open
// connections, as a restart on the same address would.
func (s *swapServer) swap(cert tls.Certificate) {
	s.cert.Store(&cert)
	s.CloseClientConnections()
}

// swapManager is a connection manager for the swap server s with the pin
// fp; it collects the log and the status changes.
type swapManager struct {
	*Manager
	logs     *lockedBuf
	statusMu sync.Mutex
	changes  []Status
}

func newSwapManager(t *testing.T, s *swapServer, token, fp string) *swapManager {
	t.Helper()
	src := &fakeSource{targets: map[string]Target{
		"h1": {ID: "h1", Name: "sf", Protocol: httpapi.Protocol, Addr: s.Listener.Addr().String(), Password: logging.Secret(token), Usable: true,
			Fingerprint: fp, Dialect: "satisfactory", Parallel: 4},
	}}
	sm := &swapManager{logs: &lockedBuf{}}
	sm.Manager = New(rcon.Registry{httpapi.Protocol: httpapi.Adapter{}}, src, logging.New(sm.logs, "panel", config.Log{Format: "json"}).Module("rcon"),
		Options{ProbeInterval: 50 * time.Millisecond, MaxBackoff: 100 * time.Millisecond, IdleProbe: time.Hour,
			OnStatus: func(_ string, st Status) {
				sm.statusMu.Lock()
				sm.changes = append(sm.changes, st)
				sm.statusMu.Unlock()
			}})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); sm.Wait() })
	if err := sm.Start(ctx); err != nil {
		t.Fatal(err)
	}
	return sm
}

func (sm *swapManager) events() int {
	return strings.Count(sm.logs.String(), `"event":"`+string(logging.ServerCertMismatch)+`"`)
}

func (sm *swapManager) mismatchChanges() int {
	sm.statusMu.Lock()
	defer sm.statusMu.Unlock()
	n := 0
	for _, st := range sm.changes {
		if st == CertMismatch {
			n++
		}
	}
	return n
}

func (sm *swapManager) waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			st, _ := sm.Status("h1")
			t.Fatalf("%s: status %s", what, st)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (sm *swapManager) is(want Status) func() bool {
	return func() bool { st, _ := sm.Status("h1"); return st == want }
}

// TestCertSwapDuringConnectionC46 covers PROT-003 on an open connection:
// when the server shows another certificate while commands run, the first
// refused command sets cert_mismatch at once and logs server.cert.mismatch
// once, without a reload; further refused commands and the following
// attempts to connect log nothing more and no request reaches the game
// server. Back on the trusted certificate the server connects again, and a
// new mismatch logs again. The server closes each connection after its
// answer, so that the command after the swap starts with a handshake.
func TestCertSwapDuringConnectionC46(t *testing.T) {
	const token = "tok-of-the-api"
	cert1, fp1 := newCertificate(t)
	cert2, _ := newCertificate(t)
	s := startSwapServer(t, token, cert1, false)
	sm := newSwapManager(t, s, token, fp1)
	m, logs := sm.Manager, sm.logs
	ctx := context.Background()
	wait := func(cond func() bool, what string) { t.Helper(); sm.waitFor(t, cond, what) }
	is, events := sm.is, sm.events

	wait(is(Connected), "connect")
	if out, err := m.Execute(ctx, "h1", "hello"); err != nil || out != "ok" {
		t.Fatalf("before the swap: %q %v", out, err)
	}

	s.swap(cert2)
	before := s.requests.Load()
	if _, err := m.Execute(ctx, "h1", "hello"); !errors.Is(err, rcon.ErrCertMismatch) {
		t.Fatalf("command after the swap: %v, want ErrCertMismatch", err)
	}
	if st, _ := m.Status("h1"); st != CertMismatch {
		t.Errorf("status right after the refused command: %s, want %s", st, CertMismatch)
	}
	if n := events(); n != 1 {
		t.Errorf("server.cert.mismatch events = %d, want 1", n)
	}
	// More commands at once: each is refused, none logs again.
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := m.Execute(ctx, "h1", "hello"); !errors.Is(err, rcon.ErrCertMismatch) && !errors.Is(err, ErrUnavailable) {
				t.Errorf("further command: %v", err)
			}
		})
	}
	wg.Wait()
	// The worker tries to connect again; those attempts log nothing more.
	hs := s.handshakes.Load()
	wait(func() bool { return s.handshakes.Load() >= hs+2 }, "attempts after the mismatch")
	if n := events(); n != 1 {
		t.Errorf("server.cert.mismatch events after more refusals = %d, want 1", n)
	}
	if n := s.requests.Load() - before; n != 0 {
		t.Errorf("%d requests reached the game server after the swap", n)
	}

	// Back on the trusted certificate, then another one again.
	s.swap(cert1)
	wait(is(Connected), "reconnect with the trusted certificate")
	if out, err := m.Execute(ctx, "h1", "hello"); err != nil || out != "ok" {
		t.Fatalf("after the recovery: %q %v", out, err)
	}
	s.swap(cert2)
	before = s.requests.Load()
	if _, err := m.Execute(ctx, "h1", "hello"); !errors.Is(err, rcon.ErrCertMismatch) {
		t.Fatalf("command after the second swap: %v", err)
	}
	if st, _ := m.Status("h1"); st != CertMismatch {
		t.Errorf("status after the second swap: %s", st)
	}
	if n := events(); n != 2 {
		t.Errorf("server.cert.mismatch events after a new mismatch = %d, want 2", n)
	}
	if n := s.requests.Load() - before; n != 0 {
		t.Errorf("%d requests reached the game server after the second swap", n)
	}
	mism := sm.mismatchChanges()
	if mism != 2 {
		t.Errorf("status changes to cert_mismatch = %d, want 2", mism)
	}
	if strings.Contains(logs.String(), token) {
		t.Error("the token is in the log")
	}
}

// TestCertSwapIdleConnectionC46 covers PROT-003 with a kept-alive
// connection that the server closes when it shows another certificate. A
// command may still go out on the closed connection and fail without a
// handshake; it reaches no handler and is not repeated, since a command
// must not run twice. The next command starts with a handshake: it is
// refused for the certificate, sets cert_mismatch at once and logs
// server.cert.mismatch once.
func TestCertSwapIdleConnectionC46(t *testing.T) {
	const token = "tok-of-the-api"
	cert1, fp1 := newCertificate(t)
	cert2, _ := newCertificate(t)
	s := startSwapServer(t, token, cert1, true)
	sm := newSwapManager(t, s, token, fp1)
	ctx := context.Background()
	sm.waitFor(t, sm.is(Connected), "connect")
	if out, err := sm.Execute(ctx, "h1", "hello"); err != nil || out != "ok" {
		t.Fatalf("before the swap: %q %v", out, err)
	}

	s.swap(cert2)
	before, hs := s.requests.Load(), s.handshakes.Load()
	_, err := sm.Execute(ctx, "h1", "hello")
	if !errors.Is(err, rcon.ErrCertMismatch) {
		// Only the closed connection may give another error: no new
		// handshake took place, the status stays and nothing is logged.
		if err == nil || s.handshakes.Load() != hs {
			t.Fatalf("first command after the swap: %v with %d new handshakes", err, s.handshakes.Load()-hs)
		}
		if st, _ := sm.Status("h1"); st != Connected || sm.events() != 0 {
			t.Fatalf("after the closed connection: status %s, %d events", st, sm.events())
		}
		_, err = sm.Execute(ctx, "h1", "hello")
	}
	if !errors.Is(err, rcon.ErrCertMismatch) {
		t.Fatalf("command after the swap: %v, want ErrCertMismatch", err)
	}
	if st, _ := sm.Status("h1"); st != CertMismatch {
		t.Errorf("status right after the refused command: %s, want %s", st, CertMismatch)
	}
	if _, err := sm.Execute(ctx, "h1", "hello"); !errors.Is(err, ErrUnavailable) && !errors.Is(err, rcon.ErrCertMismatch) {
		t.Errorf("further command: %v", err)
	}
	if n := sm.events(); n != 1 {
		t.Errorf("server.cert.mismatch events = %d, want 1", n)
	}
	if n := sm.mismatchChanges(); n != 1 {
		t.Errorf("status changes to cert_mismatch = %d, want 1", n)
	}
	if n := s.requests.Load() - before; n != 0 {
		t.Errorf("%d requests reached the game server after the swap", n)
	}
	if strings.Contains(sm.logs.String(), token) {
		t.Error("the token is in the log")
	}
}
