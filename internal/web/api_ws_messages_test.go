// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestWebSocketMessages covers the topic messages (MSG-001…003, D-085):
// events only to the
// participants of a message, kind and IDs, never a text. Events come in
// order, so an event to the wrong socket shows as the wrong next kind.
func TestWebSocketMessages(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	srv := httptest.NewServer(f.h)
	t.Cleanup(srv.Close)
	w := &wsFixture{apiFixture: f, srv: srv}
	admin, mod := f.login(t, "Boss"), f.login(t, "Mod")
	const origin = "http://panel.lan"
	ca, _, err := w.dial(t, admin, w.origin())
	if err != nil {
		t.Fatal(err)
	}
	cm, _, err := w.dial(t, mod, w.origin())
	if err != nil {
		t.Fatal(err)
	}
	send(t, ca, "subscribe", "messages")
	send(t, cm, "subscribe", "messages")
	// Two log lines name the topic: the two subscriptions.
	w.subscribed(t, "messages", 2)
	rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/messages", cookies: []*http.Cookie{admin}, origin: origin,
		body: `{"text":"secret words","audience":"users","user_ids":["` + f.userID + `"]}`})
	var sent struct{ ID string }
	if err := json.Unmarshal(rec.Body.Bytes(), &sent); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("send = %d %s", rec.Code, rec.Body)
	}
	expect := func(c *websocket.Conn, who, kind string) {
		t.Helper()
		m, err := next(t, c, 5*time.Second)
		if err != nil || m.Type != "event" || !strings.Contains(string(m.Data), `"kind":"`+kind+`"`) ||
			!strings.Contains(string(m.Data), sent.ID) || strings.Contains(string(m.Data), "secret") {
			t.Fatalf("%s %s: %+v %v", who, kind, m, err)
		}
	}
	expect(cm, "recipient", "message")
	rec = f.do(t, call{method: http.MethodPost, path: "/api/v1/messages/" + sent.ID + "/comments", cookies: []*http.Cookie{mod}, origin: origin, body: `{"text":"secret reply"}`})
	var c struct{ ID string }
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("comment = %d %s", rec.Code, rec.Body)
	}
	expect(ca, "sender", "comment")
	expect(cm, "author", "comment")
	f.do(t, call{method: http.MethodDelete, path: "/api/v1/messages/" + sent.ID + "/comments/" + c.ID, cookies: []*http.Cookie{mod}, origin: origin})
	expect(ca, "sender", "comment_deleted")
	expect(cm, "author", "comment_deleted")
	f.do(t, call{method: http.MethodPost, path: "/api/v1/messages/" + sent.ID + "/read", cookies: []*http.Cookie{mod}, origin: origin})
	expect(cm, "reader", "read")
	f.do(t, call{method: http.MethodDelete, path: "/api/v1/messages/" + sent.ID, cookies: []*http.Cookie{admin}, origin: origin})
	expect(ca, "sender", "deleted")
	expect(cm, "recipient", "deleted")
}
