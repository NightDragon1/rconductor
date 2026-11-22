// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// TestMessagesOverHTTP covers MSG-001 and MSG-003 over HTTP: the limits
// of the hour answer 429 with Retry-After; recipients, senders and the
// RCON admin get 403 where the role sees the message but may not act, and
// a stranger 404 (D-085).
func TestMessagesOverHTTP(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	var hash string
	if err := f.st.DB.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, f.userID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	storetest.CreateUser(t, f.st, storetest.User{Pseudonym: "SA", Role: "server_admin", PasswordHash: hash, CreatedBy: f.adminID})
	storetest.CreateUser(t, f.st, storetest.User{Pseudonym: "Mod2", Role: "moderator", PasswordHash: hash, CreatedBy: f.adminID})
	admin, sa, mod, mod2 := f.login(t, "Boss"), f.login(t, "SA"), f.login(t, "Mod"), f.login(t, "Mod2")
	const origin = "http://panel.lan"
	as := func(who *http.Cookie, c call) call {
		c.cookies = []*http.Cookie{who}
		if c.method != http.MethodGet {
			c.origin = origin
		}
		return c
	}
	sent := func(who *http.Cookie, audience string) string {
		t.Helper()
		rec := f.do(t, as(who, call{method: http.MethodPost, path: "/api/v1/messages", body: `{"text":"hi","audience":"` + audience + `"}`}))
		if rec.Code != http.StatusCreated {
			t.Fatalf("send %s = %d %s", audience, rec.Code, rec.Body)
		}
		var m struct{ ID string }
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return m.ID
	}
	toMods := sent(admin, "moderators")
	// A message of the server admin to Mod: the RCON admin sees it, but is
	// neither its sender nor a recipient.
	const toSAs = "00000000-0000-4000-8000-0000000000aa"
	if _, err := f.st.DB.Exec(`INSERT INTO messages (id, sender_id, sender_pseudonym, audience, text, created_at)
		SELECT ?, id, 'SA', 'scope', 'hi', '2026-11-22T00:00:00.000Z' FROM users WHERE pseudonym = 'SA'`, toSAs); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.DB.Exec(`INSERT INTO message_recipients (message_id, user_id) SELECT ?, id FROM users WHERE pseudonym = 'Mod'`, toSAs); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/messages/"

	rec := f.do(t, as(mod, call{method: http.MethodPost, path: base + toMods + "/comments", body: `{"text":"mine"}`}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("comment = %d %s", rec.Code, rec.Body)
	}
	var c struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &c)

	for _, tc := range []struct {
		name string
		who  *http.Cookie
		c    call
		want int
	}{
		// a recipient deletes no message (D-085)
		{"recipient deletes", mod, call{method: http.MethodDelete, path: base + toMods}, http.StatusForbidden},
		// the RCON admin is neither sender nor recipient
		{"admin comments", admin, call{method: http.MethodPost, path: base + toSAs + "/comments", body: `{"text":"x"}`}, http.StatusForbidden},
		{"admin marks read", admin, call{method: http.MethodPost, path: base + toSAs + "/read"}, http.StatusForbidden},
		// another recipient deletes no comment
		{"other recipient deletes comment", mod2, call{method: http.MethodDelete, path: base + toMods + "/comments/" + c.ID}, http.StatusForbidden},
		// a stranger sees neither the message nor its comments
		{"stranger lists comments", sa, call{method: http.MethodGet, path: base + toMods + "/comments"}, http.StatusNotFound},
		{"stranger reads", sa, call{method: http.MethodGet, path: base + toMods}, http.StatusNotFound},
		// a comment without text field
		{"comment without text", mod, call{method: http.MethodPost, path: base + toMods + "/comments", body: `{}`}, http.StatusBadRequest},
	} {
		if got := f.do(t, as(tc.who, tc.c)).Code; got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, got, tc.want)
		}
	}

	for key, c := range map[string]call{
		"messages.per_hour":          as(admin, call{method: http.MethodPost, path: "/api/v1/messages", body: `{"text":"hi","audience":"all"}`}),
		"messages.comments_per_hour": as(mod, call{method: http.MethodPost, path: base + toMods + "/comments", body: `{"text":"again"}`}),
	} {
		if _, err := f.st.DB.Exec(`INSERT INTO settings (id, key, value, scope_type, scope_id, updated_at) VALUES (?, ?, '1', 'global', '', 't')
			ON CONFLICT DO UPDATE SET value = excluded.value`, key, key); err != nil {
			t.Fatal(err)
		}
		rec := f.do(t, c)
		if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
			t.Errorf("%s: %d, Retry-After %q, want 429 with Retry-After", key, rec.Code, rec.Header().Get("Retry-After"))
		}
	}
}
