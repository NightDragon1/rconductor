// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

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
	"sync/atomic"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/connmgr"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/rcon"
	"github.com/NightDragon1/rconductor/internal/rcon/httpapi"
	"github.com/NightDragon1/rconductor/internal/rcon/httpapi/httpapitest"
)

// swapServer is the fake game server of a dialect whose certificate can be
// replaced while it keeps its address. It counts every request that
// reaches the handler, with or without a token.
type swapServer struct {
	*httptest.Server
	cert     atomic.Pointer[tls.Certificate]
	requests atomic.Int64
}

func newCertificate(t *testing.T, name string) (tls.Certificate, string) {
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
		Subject:      pkix.Name{CommonName: name},
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

func startSwapServer(t *testing.T, f httpapitest.Fixture, first tls.Certificate) *swapServer {
	t.Helper()
	s := &swapServer{}
	s.cert.Store(&first)
	inner := f.Handler(apiToken, 0, func(string) string { return "ok" })
	s.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		inner.ServeHTTP(w, r)
	}))
	s.TLS = &tls.Config{
		Certificates: []tls.Certificate{first},
		GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
			return &tls.Config{Certificates: []tls.Certificate{*s.cert.Load()}, NextProtos: []string{"http/1.1"}}, nil
		},
	}
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}

// swap makes the server show cert from the next handshake on and drops
// the open connections, as a restart on the same address would.
func (s *swapServer) swap(cert tls.Certificate) {
	s.cert.Store(&cert)
	s.CloseClientConnections()
}

func mismatchEvents(logs string) int {
	return strings.Count(logs, `"event":"`+string(logging.ServerCertMismatch)+`"`)
}

// TestPROT003CertificateSwapConnectionTest: after the admin trusted the
// certificate seen in the first connection test, the server shows another
// certificate with a new key on the same address. The connection test of
// the stored server and of the typed-in data with the old pin give
// cert_mismatch with the new fingerprint, no request reaches the game
// server, server.cert.mismatch is logged and the token is not in the log.
func TestPROT003CertificateSwapConnectionTest(t *testing.T) {
	h := newHTTPFixture(t)
	ctx := context.Background()
	for _, d := range httpapi.Names() {
		cert1, fp1 := newCertificate(t, "first")
		cert2, fp2 := newCertificate(t, "second")
		if fp1 == fp2 {
			t.Fatal("same fingerprint for two keys")
		}
		s := startSwapServer(t, httpapitest.Fixtures[d], cert1)
		host, port := hostPort(t, s.Listener.Addr().String())
		profile := h.profile(t, d)
		in := ConnectionData{ProfileIDs: &[]string{profile}, Host: &host, Port: &port, Password: secretOf(apiToken)}

		// First test: the certificate is only read; the admin confirms it.
		got, err := h.svc.TestConnection(ctx, h.f.as("admin"), in)
		if err != nil || got.Result != ConnectionUnpinned || got.Fingerprint != fp1 {
			t.Fatalf("%s first test: %+v %v", d, got, err)
		}
		if n := s.requests.Load(); n != 0 {
			t.Fatalf("%s: %d requests before the pin", d, n)
		}
		in.TLSFingerprint = &fp1
		if got, err := h.svc.TestConnection(ctx, h.f.as("admin"), in); err != nil || got.Result != ConnectionOK {
			t.Fatalf("%s pinned test: %+v %v", d, got, err)
		}
		name := "swap-test-" + d
		sv, err := h.svc.Create(ctx, h.f.as("admin"), Input{Name: &name, ProfileIDs: &[]string{profile}, Host: &host, Port: &port,
			Password: secretOf(apiToken), TLSFingerprint: &fp1})
		if err != nil {
			t.Fatal(err)
		}
		waitStatus(t, h.svc, sv.ID, connmgr.Connected)

		s.swap(cert2)
		before := s.requests.Load()
		logsBefore := mismatchEvents(h.logs.String())

		// The stored server with its pin, and the typed-in data with the old
		// pin.
		for _, c := range []ConnectionData{
			{ServerID: sv.ID},
			{ServerID: sv.ID, Password: secretOf(apiToken)},
			{ProfileIDs: &[]string{profile}, Host: &host, Port: &port, Password: secretOf(apiToken), TLSFingerprint: &fp1},
		} {
			got, err := h.svc.TestConnection(ctx, h.f.as("admin"), c)
			if err != nil || got.Result != ConnectionCertMismatch || got.Fingerprint != fp2 {
				t.Errorf("%s test after the swap (%+v): %+v %v", d, c.ServerID, got, err)
			}
		}
		if n := s.requests.Load() - before; n != 0 {
			t.Errorf("%s: %d requests reached the game server after the swap", d, n)
		}
		if n := mismatchEvents(h.logs.String()) - logsBefore; n < 3 {
			t.Errorf("%s: %d server.cert.mismatch events for 3 tests", d, n)
		}
	}
	if strings.Contains(h.logs.String(), apiToken) {
		t.Error("the token is in the log")
	}
}

// TestPROT003CertificateSwapExecute: a connected server with the pinned
// certificate; the server then shows another certificate with a new key on
// the same address. A command is refused with ErrCertMismatch without a
// request reaching the game server; after a reload the status is
// cert_mismatch, server.cert.mismatch is logged, further commands are
// refused and still no request arrives; the token is not in the log.
func TestPROT003CertificateSwapExecute(t *testing.T) {
	h := newHTTPFixture(t)
	ctx := context.Background()
	m := h.svc.conns.(*connmgr.Manager)
	for _, d := range httpapi.Names() {
		cert1, fp1 := newCertificate(t, "first")
		cert2, _ := newCertificate(t, "second")
		s := startSwapServer(t, httpapitest.Fixtures[d], cert1)
		host, port := hostPort(t, s.Listener.Addr().String())
		profile := h.profile(t, d)
		name := "swap-exec-" + d
		sv, err := h.svc.Create(ctx, h.f.as("admin"), Input{Name: &name, ProfileIDs: &[]string{profile}, Host: &host, Port: &port,
			Password: secretOf(apiToken), TLSFingerprint: &fp1})
		if err != nil {
			t.Fatal(err)
		}
		waitStatus(t, h.svc, sv.ID, connmgr.Connected)
		if out, err := m.Execute(ctx, sv.ID, "hello"); err != nil || out != "ok" {
			t.Fatalf("%s before the swap: %q %v", d, out, err)
		}

		s.swap(cert2)
		before := s.requests.Load()
		logsBefore := mismatchEvents(h.logs.String())

		if _, err := m.Execute(ctx, sv.ID, "hello"); !errors.Is(err, rcon.ErrCertMismatch) {
			t.Errorf("%s command after the swap: %v, want ErrCertMismatch", d, err)
		}
		if n := s.requests.Load() - before; n != 0 {
			t.Errorf("%s: %d requests reached the game server with the command", d, n)
		}

		if err := m.Reload(ctx, sv.ID); err != nil {
			t.Fatal(err)
		}
		waitStatus(t, h.svc, sv.ID, connmgr.CertMismatch)
		if n := mismatchEvents(h.logs.String()) - logsBefore; n < 1 {
			t.Errorf("%s: no server.cert.mismatch after the reload", d)
		}
		if _, err := m.Execute(ctx, sv.ID, "hello"); !errors.Is(err, connmgr.ErrUnavailable) {
			t.Errorf("%s command at cert_mismatch: %v, want ErrUnavailable", d, err)
		}
		if n := s.requests.Load() - before; n != 0 {
			t.Errorf("%s: %d requests reached the game server after the swap", d, n)
		}
	}
	if strings.Contains(h.logs.String(), apiToken) {
		t.Error("the token is in the log")
	}
}
