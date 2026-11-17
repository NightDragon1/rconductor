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
	check(http.StatusNotImplemented, admin, ops...)
	check(http.StatusNotImplemented, sa, send(`{"text":"hi","audience":"scope"}`))
	check(http.StatusForbidden, mod, send(`{"text":"hi","audience":"all"}`))
	check(http.StatusForbidden, sa, send(`{"text":"hi","audience":"all"}`), send(`{"text":"hi","audience":"users","user_ids":[`+uid+`]}`))
	check(http.StatusBadRequest, admin, send(`{"text":"hi","audience":"users"}`), send(`{"text":"hi","audience":"all","user_ids":[`+uid+`]}`),
		send(`{"text":"hi","audience":"users","user_ids":[`+uid+`,`+uid+`]}`))
	check(http.StatusBadRequest, mod, call{method: http.MethodGet, path: "/api/v1/messages/x"})
	check(http.StatusNotImplemented, mod, comment(`{"text":"ok"}`))
	check(http.StatusBadRequest, mod, comment(`{"text":""}`), comment(`{"text":"`+strings.Repeat("é", 2049)+`"}`),
		call{method: http.MethodDelete, path: "/api/v1/messages/" + id + "/comments/x", origin: origin})
	var refused int
	if err := f.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE action = 'message.send' AND result = 'denied'`).Scan(&refused); err != nil || refused != 3 {
		t.Errorf("audited refusals = %d (%v), want 3", refused, err)
	}
}
