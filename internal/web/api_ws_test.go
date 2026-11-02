// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/commands"
	"github.com/NightDragon1/rconductor/internal/connmgr"
)

type wsMsg struct {
	Type   string          `json:"type"`
	Topic  string          `json:"topic"`
	Data   json.RawMessage `json:"data"`
	Reason string          `json:"reason"`
}

type wsFixture struct {
	*apiFixture
	srv    *httptest.Server
	server string // a game server ID
	group  string // server group with that server
}

func newWSFixture(t *testing.T) *wsFixture {
	t.Helper()
	f := newAPIFixture(t, Proxies{})
	srv := httptest.NewServer(f.h)
	t.Cleanup(srv.Close)
	w := &wsFixture{apiFixture: f, srv: srv}
	admin := f.login(t, "Boss")
	rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/profiles", cookies: []*http.Cookie{admin}})
	var pl api.ProfileList
	_ = json.Unmarshal(rec.Body.Bytes(), &pl)
	rec = f.do(t, call{method: http.MethodPost, path: "/api/v1/servers", cookies: []*http.Cookie{admin}, origin: "http://panel.lan",
		body: `{"name":"mc","profile_id":"` + pl.Items[0].Id.String() + `","host":"mc.lan","password":"x"}`})
	var s api.Server
	_ = json.Unmarshal(rec.Body.Bytes(), &s)
	w.server = s.Id.String()
	rec = f.do(t, call{method: http.MethodPost, path: "/api/v1/server-groups", cookies: []*http.Cookie{admin}, origin: "http://panel.lan",
		body: `{"name":"mc","server_ids":["` + w.server + `"]}`})
	var g api.ServerGroup
	_ = json.Unmarshal(rec.Body.Bytes(), &g)
	w.group = g.Id.String()
	return w
}

// grantModerator gives the moderator the server group through a moderator
// group of the admin.
func (w *wsFixture) grantModerator(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO user_groups (id, name, kind, owner_id, created_at, updated_at) VALUES ('mg', 'mg', 'moderator', ?, 't', 't')`,
		`INSERT INTO user_group_members (user_group_id, user_id, added_by, created_at) VALUES ('mg', ?, ?, 't')`,
	} {
		args := []any{w.adminID}
		if strings.Contains(q, "members") {
			args = []any{w.userID, w.adminID}
		}
		if _, err := w.st.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.st.DB.ExecContext(ctx, `INSERT INTO grants (id, user_group_id, server_group_id, granted_by, created_at) VALUES ('g', 'mg', ?, ?, 't')`, w.group, w.adminID); err != nil {
		t.Fatal(err)
	}
	w.checker.Invalidate()
}

// dial opens a socket; status is the HTTP status of a refused handshake.
func (w *wsFixture) dial(t *testing.T, cookie *http.Cookie, origin string) (c *websocket.Conn, status int, err error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := http.Header{}
	h.Set("Origin", origin)
	if cookie != nil {
		h.Set("Cookie", cookie.Name+"="+cookie.Value)
	}
	c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(w.srv.URL, "http")+"/api/v1/ws", &websocket.DialOptions{HTTPHeader: h})
	if resp != nil {
		status = resp.StatusCode
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}
	if c != nil {
		t.Cleanup(func() { _ = c.CloseNow() })
	}
	return c, status, err
}

func (w *wsFixture) origin() string { return w.srv.URL }

func send(t *testing.T, c *websocket.Conn, typ, topic string) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"type": typ, "topic": topic})
	if err := c.Write(context.Background(), websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

// next returns the next message that is not a subscription confirmation.
func next(t *testing.T, c *websocket.Conn, wait time.Duration) (wsMsg, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	for {
		m, err := read(ctx, c)
		if err != nil || m.Type != "subscribed" {
			return m, err
		}
	}
}

func read(ctx context.Context, c *websocket.Conn) (wsMsg, error) {
	_, data, err := c.Read(ctx)
	if err != nil {
		return wsMsg{}, err
	}
	var m wsMsg
	return m, json.Unmarshal(data, &m)
}

// subscribed waits until the hub has the subscription.
func (w *wsFixture) subscribed(t *testing.T, topic string, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for strings.Count(w.logs.String(), `"topic":"`+topic+`"`) < n {
		if time.Now().After(deadline) {
			t.Fatalf("subscription to %s not registered", topic)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWebSocketEvents(t *testing.T) {
	w := newWSFixture(t)
	admin := w.login(t, "Boss")
	c, _, err := w.dial(t, admin, w.origin())
	if err != nil {
		t.Fatal(err)
	}
	topic := "server." + w.server + ".console"
	send(t, c, "subscribe", topic)
	w.subscribed(t, topic, 1)
	if !strings.Contains(w.logs.String(), `"event":"ws.subscribe"`) {
		t.Error("ws.subscribe missing")
	}
	w.hub.PublishConsole(w.server, commands.Entry{RequestID: "r1", UserID: w.userID, Command: "list", Response: "<b>x</b>", Result: "ok"})
	m, err := next(t, c, 5*time.Second)
	if err != nil || m.Type != "event" || m.Topic != topic || !strings.Contains(string(m.Data), `"command":"list"`) {
		t.Fatalf("event = %+v %v", m, err)
	}
	status := "server." + w.server + ".status"
	send(t, c, "subscribe", status)
	w.subscribed(t, status, 1)
	w.hub.PublishStatus(w.server, connmgr.Connected, time.Now())
	if m, err := next(t, c, 5*time.Second); err != nil || m.Topic != status || !strings.Contains(string(m.Data), `"status":"connected"`) {
		t.Errorf("status event = %+v %v", m, err)
	}
	for _, bad := range []struct{ topic, reason string }{
		{"server." + w.server + ".logs", "unsupported"},
		{"nonsense", "invalid"},
	} {
		send(t, c, "subscribe", bad.topic)
		if m, err := next(t, c, 5*time.Second); err != nil || m.Type != "error" || m.Reason != bad.reason {
			t.Errorf("%s: %+v %v", bad.topic, m, err)
		}
	}
}

func TestWebSocketScopeAndOrigin(t *testing.T) {
	w := newWSFixture(t)
	mod := w.login(t, "Mod")
	if _, status, err := w.dial(t, mod, "http://evil.example"); err == nil || status != http.StatusForbidden {
		t.Errorf("foreign origin: %v", err)
	}
	if _, status, err := w.dial(t, nil, w.origin()); err == nil || status != http.StatusUnauthorized {
		t.Errorf("without session: %v", err)
	}
	c, _, err := w.dial(t, mod, w.origin())
	if err != nil {
		t.Fatal(err)
	}
	send(t, c, "subscribe", "server."+w.server+".status")
	if m, err := next(t, c, 5*time.Second); err != nil || m.Type != "error" || m.Reason != "not_found" {
		t.Errorf("out of scope: %+v %v", m, err)
	}
	if !strings.Contains(w.logs.String(), `"event":"ws.subscribe.denied"`) {
		t.Error("ws.subscribe.denied missing")
	}
}

// TestModeratorSeesOwnCommandsOnly: in R1 moderators get only their own
// commands live (Design 6.3).
func TestModeratorSeesOwnCommandsOnly(t *testing.T) {
	w := newWSFixture(t)
	w.grantModerator(t)
	c, _, err := w.dial(t, w.login(t, "Mod"), w.origin())
	if err != nil {
		t.Fatal(err)
	}
	topic := "server." + w.server + ".console"
	send(t, c, "subscribe", topic)
	w.subscribed(t, topic, 1)
	w.hub.PublishConsole(w.server, commands.Entry{RequestID: "other", UserID: w.adminID, Command: "op x", Result: "ok"})
	w.hub.PublishConsole(w.server, commands.Entry{RequestID: "own", UserID: w.userID, Command: "list", Result: "ok"})
	m, err := next(t, c, 5*time.Second)
	if err != nil || !strings.Contains(string(m.Data), `"request_id":"own"`) {
		t.Errorf("moderator got %+v %v", m, err)
	}
}

// TestSubscriptionWithdrawn covers revision 2.2 candidate 4: a server
// group change withdraws a subscription at once with error, while the
// session stays.
func TestSubscriptionWithdrawn(t *testing.T) {
	w := newWSFixture(t)
	w.grantModerator(t)
	mod := w.login(t, "Mod")
	c, _, err := w.dial(t, mod, w.origin())
	if err != nil {
		t.Fatal(err)
	}
	topic := "server." + w.server + ".status"
	send(t, c, "subscribe", topic)
	w.subscribed(t, topic, 1)

	admin := w.login(t, "Boss")
	rec := w.do(t, call{method: http.MethodPatch, path: "/api/v1/server-groups/" + w.group, cookies: []*http.Cookie{admin},
		origin: "http://panel.lan", body: `{"server_ids":[]}`})
	if rec.Code != http.StatusOK {
		t.Fatalf("group change = %d %s", rec.Code, rec.Body.String())
	}
	m, err := next(t, c, 5*time.Second)
	if err != nil || m.Type != "error" || m.Topic != topic || m.Reason != "revoked" {
		t.Fatalf("after the group change: %+v %v", m, err)
	}
	if rec := w.do(t, call{method: http.MethodGet, path: "/api/v1/auth/session", cookies: []*http.Cookie{mod}}); rec.Code != http.StatusOK {
		t.Errorf("session ended: %d", rec.Code)
	}
	// The socket itself stays open for other topics.
	send(t, c, "subscribe", "servers.overview")
	w.subscribed(t, "servers.overview", 1)
}

// TestSessionEndClosesSocket covers the WebSocket part of AUTH-006.
func TestSessionEndClosesSocket(t *testing.T) {
	w := newWSFixture(t)
	admin := w.login(t, "Boss")
	c, _, err := w.dial(t, admin, w.origin())
	if err != nil {
		t.Fatal(err)
	}
	if rec := w.do(t, call{method: http.MethodPost, path: "/api/v1/auth/logout", cookies: []*http.Cookie{admin}, origin: "http://panel.lan"}); rec.Code != http.StatusNoContent {
		t.Fatalf("logout = %d", rec.Code)
	}
	if _, err := next(t, c, 5*time.Second); err == nil {
		t.Error("socket still open after logout")
	} else if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Errorf("close = %v", err)
	}
}

func (w *wsFixture) setLimits(t *testing.T, admin *http.Cookie, body string) int {
	t.Helper()
	return w.do(t, call{method: http.MethodPatch, path: "/api/v1/settings", cookies: []*http.Cookie{admin}, origin: "http://panel.lan", body: body}).Code
}

// TestWebSocketLimitSettings: only the RCON admin changes the limits,
// within their ranges (SEC-008, revision 2.2 candidate 10).
func TestWebSocketLimitSettings(t *testing.T) {
	w := newWSFixture(t)
	admin := w.login(t, "Boss")
	rec := w.do(t, call{method: http.MethodGet, path: "/api/v1/settings", cookies: []*http.Cookie{admin}})
	if !strings.Contains(rec.Body.String(), `"ws.max_sockets_per_session":20`) || !strings.Contains(rec.Body.String(), `"ws.max_subscriptions_per_socket":16`) {
		t.Errorf("defaults = %s", rec.Body.String())
	}
	for body, want := range map[string]int{
		`{"ws.max_sockets_per_session":0}`:       http.StatusUnprocessableEntity,
		`{"ws.max_sockets_per_session":101}`:     http.StatusUnprocessableEntity,
		`{"ws.max_subscriptions_per_socket":65}`: http.StatusUnprocessableEntity,
		`{"ws.max_subscriptions_per_socket":64}`: http.StatusOK,
		`{"ws.max_sockets_per_session":100}`:     http.StatusOK,
	} {
		if got := w.setLimits(t, admin, body); got != want {
			t.Errorf("%s = %d, want %d", body, got, want)
		}
	}
	w.grantModerator(t)
	mod := w.login(t, "Mod")
	if got := w.setLimits(t, mod, `{"ws.max_sockets_per_session":1}`); got != http.StatusForbidden {
		t.Errorf("moderator changes the limit: %d", got)
	}
}

// TestSubscriptionLimitPerSocket: over the limit a socket gets error limit
// instead of another subscription; another socket of the same session
// has its own allowance (SEC-008).
func TestSubscriptionLimitPerSocket(t *testing.T) {
	w := newWSFixture(t)
	admin := w.login(t, "Boss")
	if got := w.setLimits(t, admin, `{"ws.max_subscriptions_per_socket":2}`); got != http.StatusOK {
		t.Fatalf("set limit = %d", got)
	}
	c, _, err := w.dial(t, admin, w.origin())
	if err != nil {
		t.Fatal(err)
	}
	for _, tp := range []string{"servers.overview", "server." + w.server + ".status"} {
		send(t, c, "subscribe", tp)
		w.subscribed(t, tp, 1)
	}
	console := "server." + w.server + ".console"
	send(t, c, "subscribe", console)
	if m, err := next(t, c, 5*time.Second); err != nil || m.Type != "error" || m.Reason != "limit" || m.Topic != console {
		t.Errorf("third subscription on one socket: %+v %v", m, err)
	}
	if !strings.Contains(w.logs.String(), `"reason":"limit"`) {
		t.Error("ws.subscribe.denied with limit missing")
	}
	c2, _, err := w.dial(t, admin, w.origin())
	if err != nil {
		t.Fatal(err)
	}
	send(t, c2, "subscribe", console)
	w.subscribed(t, console, 2) // the refusal above names the topic once
	w.hub.PublishConsole(w.server, commands.Entry{RequestID: "r", UserID: w.adminID, Command: "list", Result: "ok"})
	if m, err := next(t, c2, 5*time.Second); err != nil || m.Type != "event" {
		t.Errorf("second socket: %+v %v", m, err)
	}
}

// TestOldestSocketReplaced: over the socket limit the oldest socket of the
// session closes with error replaced and the new one works.
func TestOldestSocketReplaced(t *testing.T) {
	w := newWSFixture(t)
	admin := w.login(t, "Boss")
	if got := w.setLimits(t, admin, `{"ws.max_sockets_per_session":2}`); got != http.StatusOK {
		t.Fatalf("set limit = %d", got)
	}
	first, _, err := w.dial(t, admin, w.origin())
	if err != nil {
		t.Fatal(err)
	}
	send(t, first, "subscribe", "servers.overview")
	w.subscribed(t, "servers.overview", 1)
	second, _, err := w.dial(t, admin, w.origin())
	if err != nil {
		t.Fatal(err)
	}
	third, _, err := w.dial(t, admin, w.origin())
	if err != nil {
		t.Fatal(err)
	}
	m, err := next(t, first, 5*time.Second)
	if err != nil || m.Type != "error" || m.Reason != "replaced" {
		t.Fatalf("oldest socket: %+v %v", m, err)
	}
	if _, err := next(t, first, 5*time.Second); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Errorf("oldest socket not closed: %v", err)
	}
	for _, c := range []*websocket.Conn{second, third} {
		send(t, c, "subscribe", "server."+w.server+".status")
	}
	w.subscribed(t, "server."+w.server+".status", 2)
	w.hub.PublishStatus(w.server, connmgr.Offline, time.Now())
	for name, c := range map[string]*websocket.Conn{"second": second, "third": third} {
		if m, err := next(t, c, 5*time.Second); err != nil || m.Type != "event" {
			t.Errorf("%s socket after the replacement: %+v %v", name, m, err)
		}
	}
}

// TestAgentTopicsAdminsOnly covers AGT-008 and T-014 on the WebSocket:
// metrics and logs are agent functions; a moderator with the server in
// scope is refused with forbidden.
func TestAgentTopicsAdminsOnly(t *testing.T) {
	w := newWSFixture(t)
	w.grantModerator(t)
	c, _, err := w.dial(t, w.login(t, "Mod"), w.origin())
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"metrics", "logs"} {
		topic := "server." + w.server + "." + kind
		send(t, c, "subscribe", topic)
		if m, err := next(t, c, 5*time.Second); err != nil || m.Type != "error" || m.Topic != topic || m.Reason != "forbidden" {
			t.Errorf("%s: %+v %v", kind, m, err)
		}
	}
}

// TestSubscribedConfirmation covers issue #99: a subscription is confirmed
// once with its topic before its first event; a refused one gets only
// error.
func TestSubscribedConfirmation(t *testing.T) {
	w := newWSFixture(t)
	c, _, err := w.dial(t, w.login(t, "Boss"), w.origin())
	if err != nil {
		t.Fatal(err)
	}
	topic := "server." + w.server + ".status"
	send(t, c, "subscribe", topic)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, err := read(ctx, c)
	if err != nil || m.Type != "subscribed" || m.Topic != topic {
		t.Fatalf("first message = %+v %v", m, err)
	}
	w.hub.PublishStatus(w.server, connmgr.Offline, time.Now())
	if m, err := read(ctx, c); err != nil || m.Type != "event" {
		t.Errorf("then = %+v %v", m, err)
	}
	send(t, c, "subscribe", topic) // again: no second confirmation
	send(t, c, "subscribe", "server.nope.status")
	if m, err := read(ctx, c); err != nil || m.Type != "error" || m.Topic != "server.nope.status" {
		t.Errorf("refused = %+v %v", m, err)
	}
}

// TestOverviewRuntimeReduced covers UI-006 on the WebSocket: in
// servers.overview a moderator gets runtime data without the agent part.
func TestOverviewRuntimeReduced(t *testing.T) {
	w := newWSFixture(t)
	w.grantModerator(t)
	c, _, err := w.dial(t, w.login(t, "Mod"), w.origin())
	if err != nil {
		t.Fatal(err)
	}
	send(t, c, "subscribe", "servers.overview")
	w.subscribed(t, "servers.overview", 1)
	w.hub.PublishRuntime(w.server, map[string]string{"kind": "runtime", "container": "secret"}, map[string]string{"kind": "runtime"})
	m, err := next(t, c, 5*time.Second)
	if err != nil || m.Type != "event" || strings.Contains(string(m.Data), "container") {
		t.Errorf("moderator got %+v %v", m, err)
	}
}
