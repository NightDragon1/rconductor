// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/NightDragon1/rconductor/internal/rcon/rcontest"
)

// client is one logged-in user of a running panel.
type client struct {
	t      *testing.T
	base   string
	cookie *http.Cookie
}

// call sends body as JSON and decodes the answer into out; it returns the
// status.
func (c *client) call(method, path string, body, out any) int {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, c.base+"/api/v1"+path, rd)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", c.base)
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	if method == http.MethodPatch && versionedPath.MatchString(path) {
		if v := c.version(path); v != "" {
			req.Header.Set("If-Match", v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if c.cookie == nil {
		for _, ck := range resp.Cookies() {
			if ck.Name == "rcd_session" {
				c.cookie = ck
			}
		}
	}
	if out != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			c.t.Fatalf("%s %s: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

func (c *client) must(method, path string, body, out any, want int) {
	c.t.Helper()
	if got := c.call(method, path, body, out); got != want {
		c.t.Fatalf("%s %s = %d, want %d", method, path, got, want)
	}
}

func login(t *testing.T, base, pseudonym, password string) *client {
	t.Helper()
	c := &client{t: t, base: base}
	c.must(http.MethodPost, "/auth/login", map[string]string{"pseudonym": pseudonym, "password": password}, nil, http.StatusOK)
	return c
}

// stack is a running panel with one game server and three users who may
// send any command to it: the RCON admin and two server admins.
type stack struct {
	t    *testing.T
	base string
	// agentAddr is the panel's agent port.
	agentAddr string
	logs      *syncBuffer
	server    string
	users     map[string]*client
}

// newStack starts the panel and connects it to the RCON server at addr.
func newStack(t *testing.T, addr, rconPassword string) *stack {
	t.Helper()
	cfg := testConfig(t, t.TempDir())
	var out, console syncBuffer
	a, stop := startPanel(t, cfg, &out, &console)
	t.Cleanup(func() { _ = stop() })
	s := &stack{t: t, base: "http://" + a, logs: &out, users: map[string]*client{}}
	if v, ok := agentAddrs.Load(cfg.SecretsDir); ok {
		s.agentAddr = v.(string)
	}

	m := regexp.MustCompile(`password: (\S+)`).FindStringSubmatch(console.String())
	if m == nil {
		t.Fatalf("no initial password:\n%s", console.String())
	}
	admin := login(t, s.base, "admin", m[1])
	admin.must(http.MethodPost, "/me/password", map[string]string{"current_password": m[1], "new_password": "the admin's own password"}, nil, http.StatusNoContent)
	s.users["admin"] = admin

	var profiles struct {
		Items []struct{ ID, Slug string }
	}
	admin.must(http.MethodGet, "/profiles", nil, &profiles, http.StatusOK)
	var profile string
	for _, p := range profiles.Items {
		if p.Slug == "minecraft-java" {
			profile = p.ID
		}
	}
	host, port, _ := net.SplitHostPort(addr)
	p, _ := strconv.Atoi(port)
	var srv struct{ ID string }
	admin.must(http.MethodPost, "/servers", map[string]any{
		"name": "mc", "profile_id": profile, "host": host, "port": p, "password": rconPassword,
	}, &srv, http.StatusCreated)
	s.server = srv.ID
	// The rate limits of Design 3.4 would stop a burst of 20 commands.
	admin.must(http.MethodPatch, "/settings", map[string]int{
		"ratelimit.rcon_admin.burst": 100, "ratelimit.server_admin.burst": 100,
	}, nil, http.StatusOK)

	for _, name := range []string{"sa1", "sa2"} {
		var inv struct{ Token string }
		admin.must(http.MethodPost, "/invites", map[string]string{"role": "server_admin"}, &inv, http.StatusCreated)
		var u struct{ ID string }
		anon := &client{t: t, base: s.base}
		pw := "password of " + name
		anon.must(http.MethodPost, "/invites/redeem", map[string]string{"token": inv.Token, "pseudonym": name, "password": pw}, &u, http.StatusCreated)
		admin.must(http.MethodPut, "/users/"+u.ID+"/assignments", map[string]any{
			"admin_scopes": []map[string]string{{"server_id": s.server}},
		}, nil, http.StatusOK)
		s.users[name] = login(t, s.base, name, pw)
	}
	s.waitStatus("connected")
	return s
}

func (s *stack) waitStatus(want string) {
	s.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var srv struct{ Status string }
		s.users["admin"].must(http.MethodGet, "/servers/"+s.server, nil, &srv, http.StatusOK)
		if srv.Status == want {
			return
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("server status %s, want %s", srv.Status, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

type commandResult struct {
	RequestID string `json:"request_id"`
	Command   string `json:"command"`
	Response  string `json:"response"`
	Truncated bool   `json:"truncated"`
}

func (s *stack) send(user, command string) (commandResult, int) {
	var r commandResult
	code := s.users[user].call(http.MethodPost, "/servers/"+s.server+"/commands", map[string]string{"command": command}, &r)
	return r, code
}

// console subscribes the user to the server's console over the WebSocket.
func (s *stack) console(user string) *websocket.Conn {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.base, "http")+"/api/v1/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {s.base}, "Cookie": {s.users[user].cookie.String()}},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		s.t.Fatal(err)
	}
	s.t.Cleanup(func() { _ = c.CloseNow() })
	topic := "server." + s.server + ".console"
	b, _ := json.Marshal(map[string]string{"type": "subscribe", "topic": topic})
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		s.t.Fatal(err)
	}
	var m struct{ Type, Topic string }
	if err := readJSON(c, &m); err != nil || m.Type != "subscribed" || m.Topic != topic {
		s.t.Fatalf("subscription: %+v %v", m, err)
	}
	return c
}

func readJSON(c *websocket.Conn, v any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// eventsOf returns the lines of the event log with the given event code.
func (s *stack) eventsOf(code string) []map[string]any {
	var out []map[string]any
	for line := range strings.Lines(s.logs.String()) {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["event"] == code {
			out = append(out, m)
		}
	}
	return out
}

// TestThroughTheStack is the walking skeleton of M2 against the RCON test
// server: the RCON admin sends list over the API and sees the answer in the
// response and on the console WebSocket (PROT-001); three users with 20
// commands each share one connection and every answer reaches its sender;
// after a game restart the panel reconnects by itself (PROT-002).
func TestThroughTheStack(t *testing.T) {
	long := strings.Repeat("A line of a long answer from the game server.\n", 200)
	game, err := rcontest.Start("rconpw", rcontest.Minecraft, func(cmd string) string {
		switch cmd {
		case "list":
			return "There are 0 of a max of 20 players online: "
		case "help":
			return long
		}
		return "done: " + cmd
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(game.Close)
	s := newStack(t, game.Addr, "rconpw")

	// PROT-001: the answer as the game server gave it, also over 4 KB.
	ws := s.console("admin")
	r, code := s.send("admin", "list")
	if code != http.StatusOK || r.Response != "There are 0 of a max of 20 players online: " {
		t.Fatalf("list = %d %+v", code, r)
	}
	var ev struct {
		Type, Topic string
		Data        commandResult
	}
	if err := readJSON(ws, &ev); err != nil || ev.Type != "event" || ev.Data.RequestID != r.RequestID || ev.Data.Response != r.Response {
		t.Errorf("console event = %+v %v", ev, err)
	}
	if r, code := s.send("admin", "help"); code != http.StatusOK || r.Response != long || len(long) <= 4096 {
		t.Errorf("long answer = %d, %d bytes, want %d", code, len(r.Response), len(long))
	}

	// PROT-002: 3 users, 20 commands each, at the same time.
	var wg sync.WaitGroup
	var mu sync.Mutex
	wrong := []string{}
	for _, user := range []string{"admin", "sa1", "sa2"} {
		for i := range 20 {
			wg.Go(func() {
				cmd := fmt.Sprintf("say %s %d", user, i)
				r, code := s.send(user, cmd)
				if code != http.StatusOK || r.Response != "done: "+cmd {
					mu.Lock()
					wrong = append(wrong, fmt.Sprintf("%s: %d %q", cmd, code, r.Response))
					mu.Unlock()
				}
			})
		}
	}
	wg.Wait()
	if len(wrong) > 0 {
		t.Errorf("answers at the wrong sender or missing: %v", wrong)
	}
	if n := game.Connections(); n != 1 {
		t.Errorf("RCON connections = %d, want 1", n)
	}
	if n := len(s.eventsOf("rcon.connected")); n != 1 {
		t.Errorf("rcon.connected %d times, want 1", n)
	}
	responses := map[any]bool{}
	for _, e := range s.eventsOf("cmd.response") {
		responses[e["request_id"]] = true
	}
	sent := s.eventsOf("cmd.sent")
	for _, e := range sent {
		if !responses[e["request_id"]] {
			t.Errorf("cmd.sent %v without cmd.response", e["request_id"])
		}
	}
	if len(sent) != 62 {
		t.Errorf("cmd.sent %d times, want 62", len(sent))
	}

	// A game restart: the panel reconnects by itself.
	game.DropConnections()
	deadline := time.Now().Add(15 * time.Second)
	for len(s.eventsOf("rcon.reconnect")) == 0 || game.Connections() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("no reconnect after the restart, %d connections", game.Connections())
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.waitStatus("connected")
	if r, code := s.send("sa1", "list"); code != http.StatusOK || r.Response == "" {
		t.Errorf("after the restart = %d %+v", code, r)
	}
}

// versionedPath names the objects saved as a whole with optimistic
// locking (revision 2.4 candidate 24).
var versionedPath = regexp.MustCompile(`^/(user-groups|command-lists|servers|server-groups)/[^/?]+$|^/settings$`)

// version reads the current version of a versioned object with the
// client's session, for If-Match; empty if the session cannot read it.
func (c *client) version(path string) string {
	c.t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, c.base+"/api/v1"+path, nil)
	if err != nil {
		c.t.Fatal(err)
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	if path == "/settings" {
		return resp.Header.Get("ETag")
	}
	var o struct{ Version string }
	_ = json.NewDecoder(resp.Body).Decode(&o)
	return o.Version
}
