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

func next(t *testing.T, c *websocket.Conn, wait time.Duration) (wsMsg, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		return wsMsg{}, err
	}
	var m wsMsg
	return m, json.Unmarshal(data, &m)
}

// subscribed waits until the hub has the subscription (it sends no ack).
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

// TestWebSocketLimits covers SEC-008: over the subscription limit the
// session gets an error instead of another subscription; over the
// connection limit the socket is closed.
func TestWebSocketLimits(t *testing.T) {
	w := newWSFixture(t)
	admin := w.login(t, "Boss")
	c, _, err := w.dial(t, admin, w.origin())
	if err != nil {
		t.Fatal(err)
	}
	topics := []string{"servers.overview", "server." + w.server + ".status", "server." + w.server + ".console"}
	for _, tp := range topics {
		send(t, c, "subscribe", tp)
		w.subscribed(t, tp, 1)
	}
	c2, _, err := w.dial(t, admin, w.origin())
	if err != nil {
		t.Fatal(err)
	}
	send(t, c2, "subscribe", "servers.overview")
	if m, err := next(t, c2, 5*time.Second); err != nil || m.Type != "error" || m.Reason != "limit" {
		t.Errorf("fourth subscription of the session: %+v %v", m, err)
	}
	if !strings.Contains(w.logs.String(), `"reason":"limit"`) {
		t.Error("ws.subscribe.denied with limit missing")
	}
	c3, _, err := w.dial(t, admin, w.origin())
	if err == nil {
		if _, err := next(t, c3, 5*time.Second); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
			t.Errorf("third socket: %v", err)
		}
	}
}
