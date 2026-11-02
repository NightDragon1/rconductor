// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/auth/password"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/store"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func testConfig(t *testing.T, root string) config.Panel {
	t.Helper()
	cfg, err := config.LoadPanel(func(k string) (string, bool) {
		v, ok := map[string]string{
			"RCD_HTTP_ADDR":   "127.0.0.1:0",
			"RCD_AGENT_ADDR":  "127.0.0.1:0",
			"RCD_DATA_DIR":    filepath.Join(root, "data"),
			"RCD_SECRETS_DIR": filepath.Join(root, "secrets"),
			"RCD_BACKUP_DIR":  filepath.Join(root, "backups"),
			"RCD_LOG_FORMAT":  "json",
			"RCD_LOG_LEVEL":   "debug",
		}[k]
		return v, ok
	}, os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// startPanel runs the panel until the returned stop function is called.
func startPanel(t *testing.T, cfg config.Panel, out, console *syncBuffer) (addr string, stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan net.Addr, 1)
	errc := make(chan error, 1)
	go func() {
		errc <- RunPanel(ctx, cfg, out, PanelOptions{
			Ready: func(a net.Addr) { ready <- a }, Console: console, HashParams: &fastHash,
			AgentReady: func(a net.Addr) { agentAddrs.Store(cfg.SecretsDir, a.String()) },
		})
	}()
	select {
	case a := <-ready:
		return a.String(), func() error { cancel(); return <-errc }
	case err := <-errc:
		cancel()
		t.Fatalf("panel did not start: %v\n%s", err, out.String())
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatalf("panel not ready in time:\n%s", out.String())
	}
	return "", nil
}

// agentAddrs holds the bound agent port per panel (by secrets directory).
var agentAddrs sync.Map

var fastHash = password.Params{Time: 1, MemoryKiB: 64, Threads: 1, SaltLen: 16, KeyLen: 32}

func events(t *testing.T, logs string) []string {
	t.Helper()
	var out []string
	sc := bufio.NewScanner(strings.NewReader(logs))
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("not a JSON line: %q", sc.Text())
		}
		for _, k := range []string{logging.KeyTS, logging.KeyLevel, logging.KeyComp} {
			if _, ok := m[k]; !ok {
				t.Errorf("line without %s: %s", k, sc.Text())
			}
		}
		// The clean-up runs concurrently with the start; its place in the
		// sequence varies.
		if e, ok := m[logging.KeyEvent].(string); ok && e != string(logging.AuditPurge) {
			out = append(out, e)
		}
	}
	return out
}

func TestPanelStartupSequence(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(t, root)

	var first, console syncBuffer
	addr, stop := startPanel(t, cfg, &first, &console)

	resp, err := http.Get("http://" + addr + "/api/v1/unknown")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || resp.Header.Get("Content-Type") != "application/problem+json" {
		t.Errorf("unknown API path: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if err := stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	got := strings.Join(events(t, first.String()), " ")
	want := "app.start db.migrate.start db.migrate.done key.created profile.imported auth.initial_password.issued key.created app.ready"
	if got != want {
		t.Errorf("first start events = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, store.FileName)); err != nil {
		t.Errorf("database missing: %v", err)
	}

	// Second start with the same directories keeps key and data.
	var second syncBuffer
	_, stop = startPanel(t, cfg, &second, &console)
	if err := stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	got = strings.Join(events(t, second.String()), " ")
	if want := "app.start db.migrate.done key.loaded key.loaded app.ready"; got != want {
		t.Errorf("second start events = %q, want %q", got, want)
	}
	// AUTH-007: the initial password appears exactly once over two starts.
	if n := strings.Count(console.String(), "password: "); n != 1 {
		t.Errorf("initial password printed %d times:\n%s", n, console.String())
	}
}

func TestPanelFailsOnBadKeyFile(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(t, root)
	cfg.MasterKeyFile = filepath.Join(root, "missing-secret")

	var out syncBuffer
	err := RunPanel(context.Background(), cfg, &out, PanelOptions{Console: io.Discard, HashParams: &fastHash})
	if err == nil {
		t.Fatal("panel started without its configured master key")
	}
	if !strings.Contains(out.String(), "panel stopped with error") {
		t.Errorf("error not logged:\n%s", out.String())
	}
}

// TestInitialPasswordLifecycle covers AUTH-003 and AUTH-007 end to end: the
// printed initial password logs the emergency admin in; after a change in
// the API it is invalid and the new one works.
func TestInitialPasswordLifecycle(t *testing.T) {
	cfg := testConfig(t, t.TempDir())
	var out, console syncBuffer
	addr, stop := startPanel(t, cfg, &out, &console)
	defer func() { _ = stop() }()

	m := regexp.MustCompile(`password: (\S+)`).FindStringSubmatch(console.String())
	if m == nil {
		t.Fatalf("no initial password:\n%s", console.String())
	}
	base := "http://" + addr
	type answer struct {
		StatusCode int
		cookies    []*http.Cookie
	}
	post := func(path, body string, cookie *http.Cookie) answer {
		t.Helper()
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", base)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return answer{StatusCode: resp.StatusCode, cookies: resp.Cookies()}
	}
	login := func(pw string) answer {
		return post("/api/v1/auth/login", `{"pseudonym":"admin","password":"`+pw+`"}`, nil)
	}

	resp := login(m[1])
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login with the initial password = %d", resp.StatusCode)
	}
	var cookie *http.Cookie
	for _, c := range resp.cookies {
		if c.Name == "rcd_session" {
			cookie = c
		}
	}
	// AUTH-010: the initial password forces a change before anything else.
	getUsers := func() int {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/api/v1/users", nil)
		req.AddCookie(cookie)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = r.Body.Close()
		return r.StatusCode
	}
	if code := getUsers(); code != http.StatusForbidden {
		t.Errorf("GET /users before the change = %d, want 403", code)
	}
	if resp := post("/api/v1/me/password", `{"current_password":"`+m[1]+`","new_password":"a much better password"}`, cookie); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("change password = %d", resp.StatusCode)
	}
	if code := getUsers(); code != http.StatusOK {
		t.Errorf("GET /users after the change = %d, want 200", code)
	}
	if resp := login(m[1]); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("initial password after the change = %d, want 401", resp.StatusCode)
	}
	if resp := login("a much better password"); resp.StatusCode != http.StatusOK {
		t.Errorf("new password = %d", resp.StatusCode)
	}
	if strings.Contains(out.String(), m[1]) || strings.Contains(out.String(), "a much better password") {
		t.Error("a password appears in the event log")
	}
	for _, ev := range []string{"auth.login.ok", "auth.login.failed"} {
		if !strings.Contains(out.String(), `"event":"`+ev+`"`) {
			t.Errorf("%s missing", ev)
		}
	}
}

// TestEmergencyAdminRecovery covers AUTH-011 over two starts: the second
// start with RCD_EMERGENCY_ADMIN_RESET=true prints a new initial password
// exactly once; the old password does not log in any more.
func TestEmergencyAdminRecovery(t *testing.T) {
	cfg := testConfig(t, t.TempDir())
	var out, console syncBuffer
	addr, stop := startPanel(t, cfg, &out, &console)
	old := regexp.MustCompile(`password: (\S+)`).FindStringSubmatch(console.String())[1]
	login := func(addr, pw string) int {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://"+addr+"/api/v1/auth/login",
			strings.NewReader(`{"pseudonym":"admin","password":"`+pw+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://"+addr)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = r.Body.Close()
		return r.StatusCode
	}
	if code := login(addr, old); code != http.StatusOK {
		t.Fatalf("first login = %d", code)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}

	cfg.EmergencyAdminReset = true
	var out2, console2 syncBuffer
	addr, stop = startPanel(t, cfg, &out2, &console2)
	defer func() { _ = stop() }()
	m := regexp.MustCompile(`password: (\S+)`).FindAllStringSubmatch(console2.String(), -1)
	if len(m) != 1 || m[0][1] == old {
		t.Fatalf("new initial password printed %d times", len(m))
	}
	if !strings.Contains(out2.String(), `"event":"auth.emergency_admin.reset"`) {
		t.Error("auth.emergency_admin.reset missing")
	}
	if code := login(addr, old); code != http.StatusUnauthorized {
		t.Errorf("old password after the reset = %d", code)
	}
	if code := login(addr, m[0][1]); code != http.StatusOK {
		t.Errorf("new password = %d", code)
	}
}
