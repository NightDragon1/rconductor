// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/features"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// TestMessagesBehindFlag: the message operations (MSG-001 to MSG-003) are
// 404 while the flag messages is off and 401 without a session. With the
// flag on, the skeleton checks role and audience (403), then the input
// (400), as in D-051, and answers 501 for the rest (contract first).
func TestMessagesBehindFlag(t *testing.T) {
	flags, err := features.New(features.Flag{Name: "messages", Operations: []string{"ListMessages", "CreateMessage", "GetMessage", "DeleteMessage", "MarkMessageRead", "ListMessageComments", "CreateMessageComment", "DeleteMessageComment"}})
	if err != nil {
		t.Fatal(err)
	}
	f := newAPIFixtureFeatures(t, Proxies{}, flags)
	var hash string
	if err := f.st.DB.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, f.userID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	storetest.CreateUser(t, f.st, storetest.User{Pseudonym: "SA", Role: "server_admin", PasswordHash: hash, CreatedBy: f.adminID})
	admin, sa, mod := f.login(t, "Boss"), f.login(t, "SA"), f.login(t, "Mod")
	const origin, id, uid = "http://panel.lan", "00000000-0000-4000-8000-000000000001", `"00000000-0000-4000-8000-000000000002"`
	send := func(body string) call {
		return call{method: http.MethodPost, path: "/api/v1/messages", body: body, origin: origin}
	}
	comment := func(body string) call {
		return call{method: http.MethodPost, path: "/api/v1/messages/" + id + "/comments", body: body, origin: origin}
	}
	ops := []call{
		{method: http.MethodGet, path: "/api/v1/messages"},
		send(`{"text":"hi","audience":"all"}`),
		{method: http.MethodGet, path: "/api/v1/messages/" + id},
		{method: http.MethodDelete, path: "/api/v1/messages/" + id, origin: origin},
		{method: http.MethodPost, path: "/api/v1/messages/" + id + "/read", origin: origin},
		comment(`{"text":"ok"}`),
		{method: http.MethodGet, path: "/api/v1/messages/" + id + "/comments"},
		{method: http.MethodDelete, path: "/api/v1/messages/" + id + "/comments/" + id, origin: origin},
	}
	check := func(want int, who *http.Cookie, calls ...call) {
		t.Helper()
		for _, c := range calls {
			if who != nil {
				c.cookies = []*http.Cookie{who}
			}
			if got := f.do(t, c).Code; got != want {
				t.Errorf("%s %s %s = %d, want %d", c.method, c.path, c.body, got, want)
			}
		}
	}
	check(http.StatusNotFound, admin, ops...)
	if got := f.do(t, call{method: http.MethodPatch, path: "/api/v1/settings", cookies: []*http.Cookie{admin}, origin: origin,
		body: `{"features.messages":true}`}).Code; got != http.StatusOK {
		t.Fatalf("switch flag on = %d", got)
	}
	check(http.StatusUnauthorized, nil, ops...)
	// Sending (201), the list (200), an unknown message (404) and a
	// scope without recipients (400).
	check(http.StatusCreated, admin, ops[1])
	check(http.StatusOK, admin, ops[0])
	check(http.StatusNotFound, admin, ops[2], ops[3], ops[4], ops[5])
	check(http.StatusNotImplemented, admin, ops[6], ops[7])
	check(http.StatusBadRequest, admin, call{method: http.MethodGet, path: "/api/v1/messages?limit=0"},
		call{method: http.MethodGet, path: "/api/v1/messages?limit=101"})
	check(http.StatusBadRequest, sa, send(`{"text":"hi","audience":"scope"}`))
	check(http.StatusForbidden, mod, send(`{"text":"hi","audience":"all"}`))
	check(http.StatusForbidden, sa, send(`{"text":"hi","audience":"all"}`), send(`{"text":"hi","audience":"users","user_ids":[`+uid+`]}`))
	check(http.StatusBadRequest, admin, send(`{"text":"hi","audience":"users"}`), send(`{"text":"hi","audience":"all","user_ids":[`+uid+`]}`),
		send(`{"text":"hi","audience":"users","user_ids":[`+uid+`,`+uid+`]}`))
	check(http.StatusBadRequest, mod, call{method: http.MethodGet, path: "/api/v1/messages/x"})
	check(http.StatusNotFound, mod, comment(`{"text":""}`))
	check(http.StatusBadRequest, mod, call{method: http.MethodDelete, path: "/api/v1/messages/" + id + "/comments/x", origin: origin})
	// Comments on a message to all (MSG-003): written (201), too long
	// (400).
	var mid string
	if err := f.st.DB.QueryRow(`SELECT id FROM messages`).Scan(&mid); err != nil {
		t.Fatal(err)
	}
	on := call{method: http.MethodPost, path: "/api/v1/messages/" + mid + "/comments", origin: origin}
	on.body = `{"text":"ok"}`
	check(http.StatusCreated, mod, on)
	on.body = `{"text":"` + strings.Repeat("é", 2049) + `"}`
	check(http.StatusBadRequest, mod, on)
	var refused int
	if err := f.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE action = 'message.sent' AND result = 'denied'`).Scan(&refused); err != nil || refused != 3 {
		t.Errorf("audited refusals = %d (%v), want 3", refused, err)
	}
}
