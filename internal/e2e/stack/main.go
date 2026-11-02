// SPDX-License-Identifier: AGPL-3.0-or-later

// Command stack runs a panel for the end-to-end tests of the web interface
// (Playwright): the freshly built UI from -dist, a Minecraft-like RCON test
// server, test users and a game server created over the API. With -base
// it stands behind a small proxy that strips the base path, as a reverse
// proxy does (NFR-003). The addresses and passwords go to -state as JSON.
// It is a test tool only and never part of a release.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/NightDragon1/rconductor/internal/app"
	"github.com/NightDragon1/rconductor/internal/auth/password"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/rcon/rcontest"
	"github.com/NightDragon1/rconductor/internal/webui"
)

// Passwords of the test users; this is a test tool, nothing here is real.
const (
	adminPassword = "e2e admin password" //nolint:gosec // test credentials of the test tool
	userPassword  = "e2e user password"  //nolint:gosec // test credentials of the test tool
	rconPassword  = "e2e-rcon"           //nolint:gosec // test credentials of the test tool
)

// State tells the tests where the panel is and how to log in.
type State struct {
	BaseURL string            `json:"base_url"`
	Users   map[string]string `json:"users"` // pseudonym → password
	// Reset logs in with a temporary password and must change it.
	ResetUser     string `json:"reset_user"`
	ResetPassword string `json:"reset_password"`
	ServerID      string `json:"server_id"`
	ServerName    string `json:"server_name"`
}

type buffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func main() {
	addr := flag.String("addr", "127.0.0.1:4380", "address the browser opens")
	dist := flag.String("dist", "internal/webui/dist", "built web interface")
	base := flag.String("base", "/", "base path (RCD_BASE_PATH)")
	statePath := flag.String("state", "", "file for the state JSON")
	flag.Parse()
	if err := run(*addr, *dist, *base, *statePath); err != nil {
		log.Fatal(err)
	}
}

func run(addr, dist, base, statePath string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root, err := os.MkdirTemp("", "rcd-e2e-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(root) }()
	base = "/" + strings.Trim(base, "/")
	public := "http://" + addr + strings.TrimSuffix(base, "/")
	cfg, err := config.LoadPanel(func(k string) (string, bool) {
		v, ok := map[string]string{
			"RCD_HTTP_ADDR": "127.0.0.1:0", "RCD_AGENT_ADDR": "127.0.0.1:0", "RCD_PUBLIC_URL": public, "RCD_BASE_PATH": base,
			"RCD_DATA_DIR": filepath.Join(root, "data"), "RCD_SECRETS_DIR": filepath.Join(root, "secrets"),
			"RCD_BACKUP_DIR": filepath.Join(root, "backups"), "RCD_LOG_FORMAT": "json",
		}[k]
		return v, ok
	}, os.ReadFile)
	if err != nil {
		return err
	}
	game, err := rcontest.Start(rconPassword, rcontest.Minecraft, func(cmd string) string {
		if cmd == "list" {
			return "There are 2 of a max of 20 players online: Steve, <b>Alex</b>"
		}
		return "Done: " + cmd
	})
	if err != nil {
		return err
	}
	defer game.Close()

	var logs, console buffer
	ready := make(chan net.Addr, 1)
	done := make(chan error, 1)
	fast := password.Params{Time: 1, MemoryKiB: 64, Threads: 1, SaltLen: 16, KeyLen: 32}
	go func() {
		done <- app.RunPanel(ctx, cfg, &logs, app.PanelOptions{
			UI: webui.NewAt(os.DirFS(dist), base), Ready: func(a net.Addr) { ready <- a }, Console: &console, HashParams: &fast,
		})
	}()
	var panelAddr net.Addr
	select {
	case panelAddr = <-ready:
	case err := <-done:
		return fmt.Errorf("panel: %w\n%s", err, logs.String())
	}

	// The browser talks to the proxy; it strips the base path like a
	// reverse proxy and forwards to the panel, WebSocket included.
	target, _ := url.Parse("http://" + panelAddr.String())
	proxy := httputil.NewSingleHostReverseProxy(target)
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: http.StripPrefix(strings.TrimSuffix(base, "/"), proxy), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	st, err := seed("http://"+panelAddr.String(), public, console.String(), game.Addr)
	if err != nil {
		return fmt.Errorf("seed: %w\n%s", err, logs.String())
	}
	st.BaseURL = public + "/"
	if statePath != "" {
		b, _ := json.MarshalIndent(st, "", "  ")
		if err := os.WriteFile(statePath, b, 0o600); err != nil {
			return err
		}
	}
	fmt.Printf("e2e stack ready on %s\n", st.BaseURL)
	select {
	case <-ctx.Done():
	case err := <-done:
		return err
	}
	return <-done
}

// client calls the panel API directly, with the public origin.
type client struct {
	base, origin string
	cookie       *http.Cookie
}

func (c *client) call(method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, c.base+"/api/v1"+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", c.origin)
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	for _, ck := range resp.Cookies() {
		if strings.HasSuffix(ck.Name, "rcd_session") {
			c.cookie = ck
		}
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, b)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func origin(public string) string {
	u, _ := url.Parse(public)
	return u.Scheme + "://" + u.Host
}

// seed sets the admin password and creates the test users and the game
// server.
func seed(panel, public, console, gameAddr string) (State, error) {
	m := regexp.MustCompile(`password: (\S+)`).FindStringSubmatch(console)
	if m == nil {
		return State{}, errors.New("no initial password")
	}
	o := origin(public)
	admin := &client{base: panel, origin: o}
	if err := admin.call(http.MethodPost, "/auth/login", map[string]string{"pseudonym": "admin", "password": m[1]}, nil); err != nil {
		return State{}, err
	}
	if err := admin.call(http.MethodPost, "/me/password", map[string]string{"current_password": m[1], "new_password": adminPassword}, nil); err != nil {
		return State{}, err
	}
	st := State{Users: map[string]string{"admin": adminPassword}, ServerName: "survival"}

	var profiles struct {
		Items []struct{ ID, Slug string }
	}
	if err := admin.call(http.MethodGet, "/profiles", nil, &profiles); err != nil {
		return st, err
	}
	var profile string
	for _, p := range profiles.Items {
		if p.Slug == "minecraft-java" {
			profile = p.ID
		}
	}
	host, port, _ := net.SplitHostPort(gameAddr)
	p, _ := strconv.Atoi(port)
	var srv struct{ ID string }
	if err := admin.call(http.MethodPost, "/servers", map[string]any{
		"name": st.ServerName, "profile_id": profile, "host": host, "port": p, "password": rconPassword,
	}, &srv); err != nil {
		return st, err
	}
	st.ServerID = srv.ID

	for _, u := range []struct{ name, role string }{{"sadmin", "server_admin"}, {"reset", "server_admin"}} {
		var inv struct{ Token string }
		if err := admin.call(http.MethodPost, "/invites", map[string]string{"role": u.role}, &inv); err != nil {
			return st, err
		}
		var created struct{ ID string }
		anon := &client{base: panel, origin: o}
		if err := anon.call(http.MethodPost, "/invites/redeem", map[string]string{"token": inv.Token, "pseudonym": u.name, "password": userPassword}, &created); err != nil {
			return st, err
		}
		if err := admin.call(http.MethodPut, "/users/"+created.ID+"/assignments", map[string]any{
			"admin_scopes": []map[string]string{{"server_id": st.ServerID}},
		}, nil); err != nil {
			return st, err
		}
		if u.name == "reset" {
			var reset struct{ Password string }
			if err := admin.call(http.MethodPost, "/users/"+created.ID+"/password-reset", nil, &reset); err != nil {
				return st, err
			}
			st.ResetUser, st.ResetPassword = u.name, reset.Password
			continue
		}
		st.Users[u.name] = userPassword
	}
	return st, nil
}
