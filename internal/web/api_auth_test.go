// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/agenthub"
	"github.com/NightDragon1/rconductor/internal/agents"
	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/auth"
	"github.com/NightDragon1/rconductor/internal/auth/password"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/events"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/ops/keys"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/servers"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/secret"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
	builtinprofiles "github.com/NightDragon1/rconductor/profiles"
)

const testPassword = "a long enough password"

var fastParams = password.Params{Time: 1, MemoryKiB: 64, Threads: 1, SaltLen: 16, KeyLen: 32}

type apiFixture struct {
	h       http.Handler
	userID  string
	adminID string
	proxies Proxies
	logs    *lockedBuffer
	st      *store.Store
	checker *perm.Checker
	hub     *events.Hub
}

// lockedBuffer collects log output of concurrent requests.
type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
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

func newAPIFixture(t *testing.T, proxies Proxies) *apiFixture {
	t.Helper()
	st := storetest.New(t)
	ctx := context.Background()
	if err := auth.EnsureDefaults(ctx, st); err != nil {
		t.Fatal(err)
	}
	hasher, err := password.NewHasher(fastParams, 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := hasher.Hash(ctx, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	adminID := storetest.CreateUser(t, st, storetest.User{Pseudonym: "Boss", Role: "rcon_admin", PasswordHash: hash})
	id := storetest.CreateUser(t, st, storetest.User{Pseudonym: "Mod", Role: "moderator", PasswordHash: hash, CreatedBy: adminID})
	logs := &lockedBuffer{}
	log := logging.New(logs, "panel", config.Log{Level: slog.LevelDebug, Format: "json"}).Module("web")
	if err := perm.EnsureDefaults(ctx, st); err != nil {
		t.Fatal(err)
	}
	if err := profiles.EnsureDefaults(ctx, st); err != nil {
		t.Fatal(err)
	}
	if err := profiles.NewImporter(st, log, builtinprofiles.FS(), filepath.Join(t.TempDir(), "profiles")).Sync(ctx); err != nil {
		t.Fatal(err)
	}
	svc := auth.NewService(st, hasher, log, true)
	checker := perm.NewChecker(st, log)
	us := users.NewService(st, checker, log, svc.RevokeUser)
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	box, err := secret.New(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := users.EnsureAuditDefaults(ctx, st); err != nil {
		t.Fatal(err)
	}
	hub := events.NewHub(checker, nil, svc, log, events.SettingsLimits(st))
	svc.OnSessionsEnded(hub.CloseSessions)
	panelKey, err := keys.LoadOrCreateIdentity(log, filepath.Join(t.TempDir(), keys.PanelKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	agentSvc := agents.NewService(st, log, "panel.lan:8443")
	agentSvc.SetHub(agenthub.New(panelKey, log, agenthub.Options{Registry: agentSvc}))
	h := NewHandler(Options{
		Log: log, Store: st, Auth: svc, Perm: checker, Users: us, Proxies: proxies, Hub: hub, Agents: agentSvc,
		Servers:  servers.NewService(st, checker, box, log),
		Profiles: users.NewProfiles(us, hasher, svc.RevokeOtherSessions),
	})
	return &apiFixture{h: h, userID: id, adminID: adminID, proxies: proxies, logs: logs, st: st, checker: checker, hub: hub}
}

type call struct {
	method, path, body, origin string
	cookies                    []*http.Cookie
	remote                     string
	headers                    map[string]string
}

func (f *apiFixture) do(t *testing.T, c call) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(c.method, "http://panel.lan"+c.path, strings.NewReader(c.body))
	if c.body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if c.origin != "" {
		r.Header.Set("Origin", c.origin)
	}
	if c.remote != "" {
		r.RemoteAddr = c.remote
	}
	for k, v := range c.headers {
		r.Header.Set(k, v)
	}
	for _, ck := range c.cookies {
		r.AddCookie(ck)
	}
	// Versioned writes name the version the session reads (revision 2.4
	// candidate 24), unless the test sets If-Match itself.
	if c.method == http.MethodPatch && r.Header.Get("If-Match") == "" && versionedPath.MatchString(strings.TrimPrefix(c.path, "/api/v1")) {
		read := f.do(t, call{method: http.MethodGet, path: c.path, cookies: c.cookies})
		if read.Code == http.StatusOK {
			v := read.Header().Get("ETag")
			var o struct{ Version string }
			if json.Unmarshal(read.Body.Bytes(), &o) == nil && o.Version != "" {
				v = o.Version
			}
			r.Header.Set("If-Match", v)
		}
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, r)
	return rec
}

var versionedPath = regexp.MustCompile(`^/(user-groups|command-lists|servers|server-groups)/[^/?]+$|^/settings$`)

func loginBody(pseudonym, pw string) string {
	b, _ := json.Marshal(api.LoginRequest{Pseudonym: pseudonym, Password: pw})
	return string(b)
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no cookie %s in %v", name, rec.Header().Values("Set-Cookie"))
	return nil
}

func TestLoginSessionLogout(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	const origin = "http://panel.lan"

	rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/auth/login", body: loginBody("mod", testPassword), origin: origin})
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d %s", rec.Code, rec.Body.String())
	}
	var s api.Session
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if s.User.Id.String() != f.userID || s.User.Role != api.RoleModerator || s.User.Pseudonym != "Mod" {
		t.Errorf("session = %+v", s)
	}
	c := sessionCookie(t, rec, "rcd_session")
	if !c.HttpOnly || c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != int(auth.DefaultMaxLifetime.Seconds()) {
		t.Errorf("cookie over HTTP = %+v", c)
	}

	if rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/auth/session", cookies: []*http.Cookie{c}}); rec.Code != http.StatusOK {
		t.Errorf("session with cookie = %d", rec.Code)
	}
	if rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/auth/logout", cookies: []*http.Cookie{c}, origin: origin}); rec.Code != http.StatusNoContent {
		t.Errorf("logout = %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/auth/session", cookies: []*http.Cookie{c}}); rec.Code != http.StatusUnauthorized {
		t.Errorf("session after logout = %d", rec.Code)
	}
}

func TestDefaultDeny(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	for _, c := range []call{
		{method: http.MethodGet, path: "/api/v1/auth/session"},
		{method: http.MethodGet, path: "/api/v1/auth/session", cookies: []*http.Cookie{{Name: "rcd_session", Value: "made-up"}}},
		{method: http.MethodPost, path: "/api/v1/auth/logout", origin: "http://panel.lan"},
	} {
		rec := f.do(t, c)
		var p api.Problem
		_ = json.Unmarshal(rec.Body.Bytes(), &p)
		if rec.Code != http.StatusUnauthorized || p.Reason == nil || *p.Reason != api.ReasonCodeUnauthenticated {
			t.Errorf("%s %s = %d %s", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
}

func TestLoginFailuresAreUniform(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	var bodies []string
	for _, b := range []string{loginBody("Mod", "wrong password!!"), loginBody("nobody", testPassword)} {
		rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/auth/login", body: b, origin: "http://panel.lan"})
		if rec.Code != http.StatusUnauthorized || len(rec.Result().Cookies()) != 0 {
			t.Errorf("failed login = %d, cookies %v", rec.Code, rec.Result().Cookies())
		}
		var p api.Problem
		_ = json.Unmarshal(rec.Body.Bytes(), &p)
		bodies = append(bodies, p.Title+"|"+*p.Detail)
	}
	if bodies[0] != bodies[1] {
		t.Errorf("answers differ: %q vs %q", bodies[0], bodies[1])
	}
	if rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/auth/login", body: "{", origin: "http://panel.lan"}); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed body = %d", rec.Code)
	}
}

func TestOriginCheck(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	for _, origin := range []string{"", "http://evil.example", "https://panel.lan", "null"} {
		rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/auth/login", body: loginBody("Mod", testPassword), origin: origin})
		if rec.Code != http.StatusForbidden {
			t.Errorf("origin %q = %d, want 403", origin, rec.Code)
		}
	}

	u, _ := url.Parse("https://rcon.example.net/rcon")
	f = newAPIFixture(t, Proxies{PublicURL: u})
	if rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/auth/login", body: loginBody("Mod", testPassword), origin: "http://panel.lan"}); rec.Code != http.StatusForbidden {
		t.Errorf("origin of the request host with a public URL set = %d", rec.Code)
	}
	if rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/auth/login", body: loginBody("Mod", testPassword), origin: "https://rcon.example.net"}); rec.Code != http.StatusOK {
		t.Errorf("origin of the public URL = %d %s", rec.Code, rec.Body.String())
	}
}

// TestHTTPSCookie covers decision W1: behind a trusted TLS proxy the cookie
// is Secure with the __Host- prefix, and the plain name is not accepted.
func TestHTTPSCookie(t *testing.T) {
	f := newAPIFixture(t, Proxies{Trusted: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}})
	viaProxy := map[string]string{"X-Forwarded-For": "192.0.2.10", "X-Forwarded-Proto": "https"}
	rec := f.do(t, call{
		method: http.MethodPost, path: "/api/v1/auth/login", body: loginBody("Mod", testPassword),
		origin: "https://panel.lan", remote: "10.0.0.2:4000", headers: viaProxy,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login via proxy = %d %s", rec.Code, rec.Body.String())
	}
	c := sessionCookie(t, rec, "__Host-rcd_session")
	if !c.Secure || !c.HttpOnly || c.Path != "/" || c.Domain != "" || c.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie over HTTPS = %+v", c)
	}
	ok := f.do(t, call{method: http.MethodGet, path: "/api/v1/auth/session", cookies: []*http.Cookie{c}, remote: "10.0.0.2:4000", headers: viaProxy})
	if ok.Code != http.StatusOK {
		t.Errorf("session over HTTPS = %d", ok.Code)
	}
	plain := &http.Cookie{Name: "rcd_session", Value: c.Value}
	if rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/auth/session", cookies: []*http.Cookie{plain}, remote: "10.0.0.2:4000", headers: viaProxy}); rec.Code != http.StatusUnauthorized {
		t.Errorf("plain cookie name accepted over HTTPS: %d", rec.Code)
	}
	// The same forwarding headers from an untrusted peer change nothing.
	rec = f.do(t, call{
		method: http.MethodPost, path: "/api/v1/auth/login", body: loginBody("Mod", testPassword),
		origin: "http://panel.lan", remote: "192.0.2.99:4000", headers: viaProxy,
	})
	if rec.Code != http.StatusOK || sessionCookie(t, rec, "rcd_session").Secure {
		t.Errorf("untrusted peer: %d %v", rec.Code, rec.Header().Values("Set-Cookie"))
	}
}

func TestLoginLockoutAnswer(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	for range auth.DefaultMaxFailures {
		f.do(t, call{method: http.MethodPost, path: "/api/v1/auth/login", body: loginBody("Mod", "wrong password!!"), origin: "http://panel.lan"})
	}
	rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/auth/login", body: loginBody("Mod", testPassword), origin: "http://panel.lan"})
	var p api.Problem
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != http.StatusTooManyRequests || p.Reason == nil || *p.Reason != api.ReasonCodeRateLimited {
		t.Fatalf("locked login = %d %s", rec.Code, rec.Body.String())
	}
	if ra := rec.Header().Get("Retry-After"); ra != "900" {
		t.Errorf("Retry-After = %q, want 900", ra)
	}
}

// TestReasonCodes: refusals outside commands carry the codes of Design
// 3.5 (forbidden for administration and origin).
func TestReasonCodes(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/auth/login", body: loginBody("Mod", testPassword), origin: "http://evil.example"})
	var p api.Problem
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != http.StatusForbidden || p.Reason == nil || *p.Reason != api.ReasonCodeForbidden {
		t.Errorf("origin refusal = %d %s", rec.Code, rec.Body.String())
	}
	admin := f.login(t, "Boss")
	rec = f.do(t, call{method: http.MethodPatch, path: "/api/v1/users/" + f.userID, body: `{"role":"rcon_admin"}`,
		cookies: []*http.Cookie{admin}, origin: "http://panel.lan"})
	p = api.Problem{}
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != http.StatusForbidden || p.Reason == nil || *p.Reason != api.ReasonCodeForbidden {
		t.Errorf("administration refusal = %d %s", rec.Code, rec.Body.String())
	}
	var pr *problem
	if !errors.As(errBusy(), &pr) || pr.reason != api.ReasonCodeBusy || pr.status != http.StatusServiceUnavailable || pr.retryAfter != 1 {
		t.Errorf("busy = %+v", pr)
	}
}

// countingBody records whether anyone read it.
type countingBody struct {
	io.Reader
	read *atomic.Bool
}

func (b countingBody) Read(p []byte) (int, error) {
	b.read.Store(true)
	return b.Reader.Read(p)
}

// TestWriteWithoutSessionReadsNoBody covers T-025: JSON decodes ahead of the
// session check of the operation, so a write without a session goes first.
func TestWriteWithoutSessionReadsNoBody(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	for _, c := range []struct {
		method, path string
		cookie       bool
	}{
		{http.MethodPatch, "/api/v1/settings", false},
		{http.MethodPost, "/api/v1/command-lists", false},
		{http.MethodPut, "/api/v1/me/avatar", false},
		{http.MethodPatch, "/api/v1/settings", true},
	} {
		var read atomic.Bool
		req := httptest.NewRequestWithContext(context.Background(), c.method, "http://panel.lan"+c.path, countingBody{strings.NewReader(`{"a":[[],[]]}`), &read})
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://panel.lan")
		if c.cookie {
			req.AddCookie(&http.Cookie{Name: cookieNameFor(req), Value: "made-up"})
		}
		rec := httptest.NewRecorder()
		f.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", c.method, c.path, rec.Code)
		}
		if read.Load() {
			t.Errorf("%s %s read the body without a session", c.method, c.path)
		}
	}
}

// TestPublicBodiesAreSmall covers T-025: what anyone may send is decoded, so
// its limit is far below that of signed-in users.
func TestPublicBodiesAreSmall(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	big := `{"pseudonym":"Mod","password":"` + strings.Repeat("x", MaxPublicBody) + `"}`
	for _, path := range []string{"/api/v1/auth/login", "/api/v1/invites/redeem"} {
		if rec := f.do(t, call{method: http.MethodPost, path: path, body: big, origin: "http://panel.lan"}); rec.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("%s = %d, want 413", path, rec.Code)
		}
	}
	if rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/auth/login", body: loginBody("Mod", testPassword), origin: "http://panel.lan"}); rec.Code != http.StatusOK {
		t.Errorf("normal login = %d", rec.Code)
	}
}
