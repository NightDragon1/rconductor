// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"debug/elf"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/NightDragon1/rconductor/internal/app"
	"github.com/NightDragon1/rconductor/internal/auth/password"
	"github.com/NightDragon1/rconductor/internal/webui"
)

// controlSymbol tells whether a symbol belongs to the control listener of
// the stack (control_e2e.go).
func controlSymbol(name string) bool {
	return name == "main.control" || strings.HasPrefix(name, "main.control.")
}

// controlTraces lists the control symbols of an ELF binary and whether its
// data holds the route of the control listener.
func controlTraces(t *testing.T, bin string) (syms []string, route bool) {
	t.Helper()
	f, err := elf.Open(bin)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	all, err := f.Symbols()
	if err != nil {
		t.Fatalf("%s: %v", bin, err)
	}
	for _, s := range all {
		if controlSymbol(s.Name) {
			syms = append(syms, s.Name)
		}
	}
	b, err := os.ReadFile(bin) //nolint:gosec // a binary this test built
	if err != nil {
		t.Fatal(err)
	}
	return syms, bytes.Contains(b, []byte("/pending-agent"))
}

// TestAGT005ControlOnlyWithTag builds the panel, the agent and the stack
// without tags and the stack with the tag e2e: only the last one holds the
// control listener, its symbols and its route. Four builds take about 20
// seconds with a cold build cache, so the test is skipped with -short.
func TestAGT005ControlOnlyWithTag(t *testing.T) {
	if testing.Short() {
		t.Skip("builds four binaries")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go command")
	}
	dir := t.TempDir()
	build := func(t *testing.T, name, pkg string, tags ...string) string {
		t.Helper()
		out := filepath.Join(dir, name)
		args := []string{"build", "-o", out}
		if len(tags) > 0 {
			args = append(args, "-tags", strings.Join(tags, ","))
		}
		cmd := exec.CommandContext(t.Context(), goBin, append(args, pkg)...) //nolint:gosec // fixed arguments of the test
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go build %s: %v\n%s", pkg, err, b)
		}
		return out
	}
	const mod = "github.com/NightDragon1/rconductor/"
	for _, c := range []struct {
		name, pkg string
		tags      []string
		want      bool
	}{
		{"panel", mod + "cmd/panel", nil, false},
		{"agent", mod + "cmd/agent", nil, false},
		{"stack", mod + "internal/e2e/stack", nil, false},
		{"stack-e2e", mod + "internal/e2e/stack", []string{"e2e"}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			syms, route := controlTraces(t, build(t, c.name, c.pkg, c.tags...))
			if got := len(syms) > 0; got != c.want {
				t.Errorf("control symbols %v, want present=%v", syms, c.want)
			}
			if route != c.want {
				t.Errorf("route /pending-agent in the binary: %v, want %v", route, c.want)
			}
		})
	}
}

// TestAGT005NoRouteWithoutTag starts the panel behind the stack's front as
// the untagged stack does: there is no control listener, POST
// /api/v1/pending-agent is 404 and POST /pending-agent answers as any
// unknown path, with and without base path.
func TestAGT005NoRouteWithoutTag(t *testing.T) {
	if controlEnabled {
		t.Skip("built with the tag e2e")
	}
	ctlURL, stop, err := startControl(t.Context(), "http://127.0.0.1:1", "http://127.0.0.1:1", "127.0.0.1:1", t.TempDir())
	if err != nil || ctlURL != "" {
		t.Fatalf("control listener without the tag: %q, %v", ctlURL, err)
	}
	stop()

	for _, base := range []string{"/", "/rcon/"} {
		t.Run(base, func(t *testing.T) {
			fr := httptest.NewUnstartedServer(nil)
			defer fr.Close()
			basePath := "/" + strings.Trim(base, "/")
			public := "http://" + fr.Listener.Addr().String() + strings.TrimSuffix(basePath, "/")
			cfg, err := panelConfig(t.TempDir(), public, basePath, "")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			ready, done := make(chan net.Addr, 1), make(chan error, 1)
			fast := password.Params{Time: 1, MemoryKiB: 64, Threads: 1, SaltLen: 16, KeyLen: 32}
			ui := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>t</title>")}}
			go func() {
				done <- app.RunPanel(ctx, cfg, io.Discard, app.PanelOptions{
					UI: webui.NewAt(ui, basePath), Ready: func(a net.Addr) { ready <- a },
					Console: io.Discard, HashParams: &fast,
				})
			}()
			var addr net.Addr
			select {
			case addr = <-ready:
			case err := <-done:
				cancel()
				t.Fatalf("panel: %v", err)
			case <-time.After(10 * time.Second):
				cancel()
				t.Fatal("panel not ready in time")
			}
			defer func() {
				cancel()
				if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
					t.Errorf("panel: %v", err)
				}
			}()
			fr.Config.Handler = front(basePath, addr.String())
			fr.Start()

			post := func(path string) (int, string) {
				t.Helper()
				req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, fr.URL+base+path, http.NoBody)
				req.Header.Set("Origin", "http://"+fr.Listener.Addr().String())
				resp, err := fr.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = resp.Body.Close() }()
				// The problem answer without its request ID, which differs
				// on each request.
				var problem map[string]any
				_ = json.NewDecoder(resp.Body).Decode(&problem)
				delete(problem, "request_id")
				b, _ := json.Marshal(problem)
				return resp.StatusCode, string(b)
			}
			if code, _ := post("api/v1/pending-agent"); code != http.StatusNotFound {
				t.Errorf("POST %sapi/v1/pending-agent: %d, want 404", base, code)
			}
			// Outside the API the panel serves only the interface, which
			// takes no POST: the path answers exactly as an unknown one.
			code, body := post("pending-agent")
			wantCode, wantBody := post("no-such-path")
			if code != wantCode || body != wantBody || code < 400 {
				t.Errorf("POST %spending-agent: %d %q, want as an unknown path: %d %q", base, code, body, wantCode, wantBody)
			}
		})
	}
}
