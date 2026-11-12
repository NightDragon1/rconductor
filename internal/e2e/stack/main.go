// SPDX-License-Identifier: AGPL-3.0-or-later

// Command stack runs a panel for the end-to-end tests of the web interface
// (Playwright): the freshly built UI from -dist, a Minecraft-like RCON test
// server, an in-process agent with a stand-in for Docker, test users and
// game servers created over the API. With -base
// it stands behind a small proxy that strips the base path, as a reverse
// proxy does (NFR-003). The addresses and passwords go to -state as JSON.
// It is a test tool only and never part of a release. The control listener
// for the tests (control_e2e.go) is compiled only with the build tag e2e.
package main

import (
	"bytes"
	"context"
	"database/sql"
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

	_ "modernc.org/sqlite"

	"github.com/NightDragon1/rconductor/internal/app"
	"github.com/NightDragon1/rconductor/internal/auth"
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
	// Servers holds the IDs of all game servers by name; LockedUserID is
	// a disabled supporter (UI-003).
	Servers      map[string]string `json:"servers"`
	LockedUserID string            `json:"locked_user_id"`
	// ExpiredInvite is the token of an invitation past its time (UI-008).
	ExpiredInvite string `json:"expired_invite"`
	// LegacyAgentID is host-legacy, an agent with fewer capabilities and the
	// container mc-legacy (AGT-013).
	LegacyAgentID string `json:"legacy_agent_id"`
	// ControlURL is a listener of the stack for the tests only: POST
	// /pending-agent starts a new agent with a name of its own that waits
	// for approval by fingerprint, so a test does not use up the seed.
	// It exists only in a build with the tag e2e and is empty otherwise.
	ControlURL string `json:"control_url"`
	profile    string
	moderators string // the moderator group
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
	logPath := flag.String("log", "", "file that gets the panel's log lines too")
	trusted := flag.String("trusted", "", "RCD_TRUSTED_PROXIES of the panel, as behind a configured proxy")
	host := flag.String("host", "", "host name the browser uses instead of the address, e.g. rcon.lan for a page outside a secure context")
	flag.Parse()
	if err := run(*addr, *host, *dist, *base, *trusted, *statePath, *logPath); err != nil {
		log.Fatal(err)
	}
}

func run(addr, host, dist, base, trusted, statePath, logPath string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root, err := os.MkdirTemp("", "rcd-e2e-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(root) }()
	base = "/" + strings.Trim(base, "/")
	hostport := addr
	if host != "" {
		_, port, _ := net.SplitHostPort(addr)
		hostport = net.JoinHostPort(host, port)
	}
	public := "http://" + hostport + strings.TrimSuffix(base, "/")
	cfg, err := panelConfig(root, public, base, trusted)
	if err != nil {
		return err
	}
	game, err := rcontest.Start(rconPassword, rcontest.Minecraft, func(cmd string) string {
		if cmd == "list" {
			return "There are 2 of a max of 20 players online: Steve, <b>Alex</b>"
		}
		if strings.HasPrefix(cmd, "say slowly") {
			time.Sleep(time.Second) // an answer that outlasts a switch of servers
		}
		return "Done: " + cmd
	})
	if err != nil {
		return err
	}
	defer game.Close()
	hostile, err := rcontest.Start(rconPassword, rcontest.Minecraft, func(cmd string) string {
		if cmd == "list" {
			return hostileList
		}
		return hostileAnswer
	})
	if err != nil {
		return err
	}
	defer hostile.Close()

	var logs, console buffer
	var panelLog io.Writer = &logs
	if logPath != "" {
		f, err := os.Create(logPath) //nolint:gosec // a path the test runner names
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		panelLog = io.MultiWriter(&logs, f)
	}
	ready, agentReady := make(chan net.Addr, 1), make(chan net.Addr, 1)
	done := make(chan error, 1)
	fast := password.Params{Time: 1, MemoryKiB: 64, Threads: 1, SaltLen: 16, KeyLen: 32}
	go func() {
		done <- app.RunPanel(ctx, cfg, panelLog, app.PanelOptions{
			UI: webui.NewAt(os.DirFS(dist), base), Ready: func(a net.Addr) { ready <- a },
			AgentReady: func(a net.Addr) { agentReady <- a }, Console: &console, HashParams: &fast,
		})
	}()
	var panelAddr net.Addr
	select {
	case panelAddr = <-ready:
	case err := <-done:
		return fmt.Errorf("panel: %w\n%s", err, logs.String())
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: front(base, panelAddr.String()), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	panelURL := "http://" + panelAddr.String()
	st, err := seed(panelURL, public, console.String(), game.Addr)
	if err != nil {
		return fmt.Errorf("seed: %w\n%s", err, logs.String())
	}
	admin := &client{base: panelURL, origin: origin(public)}
	if err := admin.call(http.MethodPost, "/auth/login", map[string]string{"pseudonym": "admin", "password": adminPassword}, nil); err != nil {
		return err
	}
	agentAddr := (<-agentReady).String()
	if err := seedHosts(ctx, admin, panelURL, origin(public), agentAddr, root, game.Addr, hostile.Addr, st.profile, &st); err != nil {
		return fmt.Errorf("seed hosts: %w\n%s", err, logs.String())
	}
	if st.ExpiredInvite, err = expiredInvite(ctx, admin, filepath.Join(root, "data", "rconductor.db")); err != nil {
		return fmt.Errorf("expired invite: %w", err)
	}
	ctlURL, stopCtl, err := startControl(ctx, panelURL, origin(public), agentAddr, root)
	if err != nil {
		return err
	}
	defer stopCtl()
	st.ControlURL = ctlURL
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

// panelConfig is the configuration of the stack's panel below root.
func panelConfig(root, public, base, trusted string) (config.Panel, error) {
	return config.LoadPanel(func(k string) (string, bool) {
		v, ok := map[string]string{
			"RCD_HTTP_ADDR": "127.0.0.1:0", "RCD_AGENT_ADDR": "127.0.0.1:0", "RCD_PUBLIC_URL": public, "RCD_BASE_PATH": base,
			"RCD_DATA_DIR": filepath.Join(root, "data"), "RCD_SECRETS_DIR": filepath.Join(root, "secrets"),
			"RCD_BACKUP_DIR": filepath.Join(root, "backups"), "RCD_LOG_FORMAT": "json", "RCD_TRUSTED_PROXIES": trusted,
			// Every agent of the stack connects from 127.0.0.1, and the ones
			// waiting for approval try again and again: with the default of
			// 10 per minute they would hold each other off (SEC-007).
			"RCD_AGENT_CONNECTS_PER_MINUTE": "600", "RCD_AGENT_CONNECT_BURST": "100",
		}[k]
		return v, ok
	}, os.ReadFile)
}

// front is what the browser talks to: it strips the base path like a
// reverse proxy and forwards to the panel, WebSocket included.
func front(base, panelAddr string) http.Handler {
	target, _ := url.Parse("http://" + panelAddr)
	return http.StripPrefix(strings.TrimSuffix(base, "/"), httputil.NewSingleHostReverseProxy(target))
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
	// Versioned writes name the version just read (revision 2.4
	// candidate 24).
	if method == http.MethodPatch && versionedPath.MatchString(path) {
		v, err := c.version(path)
		if err != nil {
			return err
		}
		req.Header.Set("If-Match", v)
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

var versionedPath = regexp.MustCompile(`^/(user-groups|command-lists|servers|server-groups)/[^/?]+$|^/settings$`)

// version reads the current version of a versioned object.
func (c *client) version(path string) (string, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, c.base+"/api/v1"+path, nil)
	if err != nil {
		return "", err
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if path == "/settings" {
		return resp.Header.Get("ETag"), nil
	}
	var o struct{ Version string }
	err = json.NewDecoder(resp.Body).Decode(&o)
	return o.Version, err
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
		"name": st.ServerName, "profile_ids": []string{profile}, "host": host, "port": p, "password": rconPassword,
	}, &srv); err != nil {
		return st, err
	}
	st.ServerID, st.profile = srv.ID, profile

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
	if err := seedModeration(admin, panel, o, &st); err != nil {
		return st, err
	}
	return st, nil
}

// invite creates a user with a role through an invitation by inviter.
func invite(inviter *client, panel, o, role, name string) (string, error) {
	var inv struct{ Token string }
	if err := inviter.call(http.MethodPost, "/invites", map[string]string{"role": role}, &inv); err != nil {
		return "", err
	}
	var created struct{ ID string }
	anon := &client{base: panel, origin: o}
	err := anon.call(http.MethodPost, "/invites/redeem", map[string]string{"token": inv.Token, "pseudonym": name, "password": userPassword}, &created)
	return created.ID, err
}

// seedModeration adds a moderator with a command list on the game server
// and a supporter of his.
func seedModeration(admin *client, panel, o string, st *State) error {
	var list struct{ ID string }
	if err := admin.call(http.MethodPost, "/command-lists", map[string]any{"name": "moderation", "entries": []map[string]any{
		{"path": "list", "passable": true}, {"path": "kick", "passable": false}, {"path": "whitelist add", "passable": true},
		{"path": "say", "passable": true}, {"path": "time set", "passable": true},
		// setidletimeout has no catalog entry: its card has a free field (Design 7.3).
		{"path": "setidletimeout", "passable": false},
	}}, &list); err != nil {
		return err
	}
	var group struct{ ID string }
	if err := admin.call(http.MethodPost, "/user-groups", map[string]any{"name": "moderators", "kind": "moderator", "command_list_id": list.ID}, &group); err != nil {
		return err
	}
	modID, err := invite(admin, panel, o, "moderator", "mod")
	if err != nil {
		return err
	}
	if err := admin.call(http.MethodPut, "/users/"+modID+"/assignments", map[string]any{
		"groups": []string{group.ID}, "grants": []map[string]string{{"group_id": group.ID, "server_id": st.ServerID}},
	}, nil); err != nil {
		return err
	}
	if err := admin.call(http.MethodPatch, "/settings", map[string]bool{"supporters.enabled": true}, nil); err != nil {
		return err
	}
	if err := admin.call(http.MethodPatch, "/users/"+modID, map[string]bool{"supporters_allowed": true}, nil); err != nil {
		return err
	}
	mod := &client{base: panel, origin: o}
	if err := mod.call(http.MethodPost, "/auth/login", map[string]string{"pseudonym": "mod", "password": userPassword}, nil); err != nil {
		return err
	}
	supID, err := invite(mod, panel, o, "supporter", "sup")
	if err != nil {
		return err
	}
	if err := mod.call(http.MethodPut, "/users/"+supID+"/assignments", map[string]any{
		"supporter_grants": []map[string]string{{"moderator_id": modID, "server_id": st.ServerID}},
	}, nil); err != nil {
		return err
	}
	// A second moderator without his own supporter release (Design 6.2).
	mod2ID, err := invite(admin, panel, o, "moderator", "mod2")
	if err != nil {
		return err
	}
	if err := admin.call(http.MethodPut, "/users/"+mod2ID+"/assignments", map[string]any{
		"groups": []string{group.ID}, "grants": []map[string]string{{"group_id": group.ID, "server_id": st.ServerID}},
	}, nil); err != nil {
		return err
	}
	st.Users["mod"], st.Users["mod2"], st.Users["sup"] = userPassword, userPassword, userPassword
	st.moderators = group.ID
	return nil
}

// expiredInvite creates an invitation over the API and moves its expiry
// into the past, as the shortest lifetime (one minute) is too long for a
// test.
func expiredInvite(ctx context.Context, admin *client, dbPath string) (string, error) {
	var inv struct{ Token string }
	if err := admin.call(http.MethodPost, "/invites", map[string]string{"role": "supporter"}, &inv); err != nil {
		return "", err
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()
	past := time.Now().Add(-time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")
	_, err = db.ExecContext(ctx, `UPDATE invites SET expires_at = ? WHERE token_hash = ?`, past, auth.HashToken(inv.Token))
	return inv.Token, err
}
