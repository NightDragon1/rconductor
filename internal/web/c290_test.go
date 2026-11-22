// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// TestC290CommentsOverHTTP (MSG-003): the comments of a message page with
// next_cursor, a broken cursor is 400, a comment without text is 400, an
// RCON admin who is no recipient gets 403, and past the hourly limit 429
// comes with Retry-After.
func TestC290CommentsOverHTTP(t *testing.T) {
	f := newAPIFixture(t, Proxies{})
	admin, mod := f.login(t, "Boss"), f.login(t, "Mod")
	const origin = "http://panel.lan"
	rec := f.do(t, call{method: http.MethodPost, path: "/api/v1/messages", cookies: []*http.Cookie{admin}, origin: origin,
		body: `{"text":"hi","audience":"users","user_ids":["` + f.userID + `"]}`})
	var m struct{ ID string }
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("send = %d %s", rec.Code, rec.Body)
	}
	comment := func(who *http.Cookie, body string) *httptest.ResponseRecorder {
		t.Helper()
		return f.do(t, call{method: http.MethodPost, path: "/api/v1/messages/" + m.ID + "/comments", cookies: []*http.Cookie{who}, origin: origin, body: body})
	}
	if rec := comment(mod, `{}`); rec.Code != http.StatusBadRequest {
		t.Errorf("comment without text = %d", rec.Code)
	}
	for i := range 3 {
		if rec := comment(mod, `{"text":"c`+string(rune('0'+i))+`"}`); rec.Code != http.StatusCreated {
			t.Fatalf("comment %d = %d %s", i, rec.Code, rec.Body)
		}
	}
	page := func(query string) (int, struct {
		Items      []struct{ ID string }
		NextCursor *string `json:"next_cursor"`
	}) {
		t.Helper()
		rec := f.do(t, call{method: http.MethodGet, path: "/api/v1/messages/" + m.ID + "/comments" + query, cookies: []*http.Cookie{mod}})
		var p struct {
			Items      []struct{ ID string }
			NextCursor *string `json:"next_cursor"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &p)
		return rec.Code, p
	}
	code, p := page("?limit=2")
	if code != http.StatusOK || len(p.Items) != 2 || p.NextCursor == nil {
		t.Fatalf("first page %d %+v", code, p)
	}
	if code, p2 := page("?limit=2&cursor=" + *p.NextCursor); code != http.StatusOK || len(p2.Items) != 1 || p2.NextCursor != nil {
		t.Errorf("last page %d %+v", code, p2)
	}
	if code, _ := page("?cursor=" + strings.Repeat("A", 257)); code != http.StatusBadRequest {
		t.Errorf("long cursor = %d", code)
	}
	// Another RCON admin is neither sender nor recipient: 403.
	var hash string
	if err := f.st.DB.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, f.userID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	storetest.CreateUser(t, f.st, storetest.User{Pseudonym: "Boss2", Role: "rcon_admin", PasswordHash: hash, CreatedBy: f.adminID})
	if rec := comment(f.login(t, "Boss2"), `{"text":"x"}`); rec.Code != http.StatusForbidden {
		t.Errorf("RCON admin who is no recipient = %d %s", rec.Code, rec.Body)
	}
	if rec := f.do(t, call{method: http.MethodPatch, path: "/api/v1/settings", cookies: []*http.Cookie{admin}, origin: origin,
		body: `{"messages.comments_per_hour":3}`}); rec.Code != http.StatusOK {
		t.Fatalf("limit = %d %s", rec.Code, rec.Body)
	}
	rec = comment(mod, `{"text":"one more"}`)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("past the limit = %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
}
