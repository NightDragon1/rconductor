// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"bytes"
	"context"
	"encoding/base32"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/NightDragon1/rconductor/internal/auth/totp"
	"github.com/NightDragon1/rconductor/internal/rcon/rcontest"
)

// post sends a JSON body with the cookies and returns status, body and the
// cookies the answer set.
func post(t *testing.T, base, path string, body any, cookies ...*http.Cookie) (int, []byte, []*http.Cookie) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, base+"/api/v1"+path, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", base)
	for _, c := range cookies {
		if c != nil {
			req.AddCookie(c)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out, resp.Cookies()
}

func cookieNamed(cs []*http.Cookie, name string) *http.Cookie {
	var found *http.Cookie
	for _, c := range cs {
		if c.Name == name {
			found = c
		}
	}
	return found
}

// codeAt is the code of the secret now plus offset steps; the panel's
// clock is the real one.
func codeAt(t *testing.T, secret string, offset int64) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	return totp.Code(key, totp.Step(time.Now())+offset)
}

// TestSecondFactorThroughTheStack covers AUTH-008 over HTTP: a user sets up
// the second factor with the password; the login then needs two steps, the
// second with the cookie rcd_pending; the session sent along ends only with
// the code (D-052); a manager resets the second factor and the sessions of
// the user end.
func TestSecondFactorThroughTheStack(t *testing.T) {
	game, err := rcontest.Start("rconpw", rcontest.Minecraft, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(game.Close)
	s := newStack(t, game.Addr, "rconpw")
	sa := s.users["sa1"]

	if code := sa.call(http.MethodPost, "/me/second-factor/setup", map[string]string{"password": "wrong"}, nil); code != http.StatusUnprocessableEntity {
		t.Errorf("setup with a wrong password = %d", code)
	}
	var setup struct{ Secret, URI string }
	sa.must(http.MethodPost, "/me/second-factor/setup", map[string]string{"password": "password of sa1"}, &setup, http.StatusOK)
	sa.must(http.MethodPost, "/me/second-factor/confirm", map[string]string{"code": codeAt(t, setup.Secret, 0)}, nil, http.StatusNoContent)
	var sf struct{ Enabled bool }
	sa.must(http.MethodGet, "/me/second-factor", nil, &sf, http.StatusOK)
	if !sf.Enabled {
		t.Fatal("second factor not on")
	}

	// Two steps; the session sent along stays until the code.
	status, body, cookies := post(t, s.base, "/auth/login", map[string]string{"pseudonym": "sa1", "password": "password of sa1"}, sa.cookie)
	pending := cookieNamed(cookies, "rcd_pending")
	if status != http.StatusAccepted || pending == nil || !pending.HttpOnly || cookieNamed(cookies, "rcd_session") != nil {
		t.Fatalf("first step = %d %s %+v", status, body, cookies)
	}
	if code := sa.call(http.MethodGet, "/auth/session", nil, nil); code != http.StatusOK {
		t.Errorf("session sent along after the password = %d", code)
	}
	if status, _, _ := post(t, s.base, "/auth/login/totp", map[string]string{"code": "000000"}, pending); status != http.StatusUnauthorized {
		t.Errorf("wrong code = %d", status)
	}
	status, body, cookies = post(t, s.base, "/auth/login/totp", map[string]string{"code": codeAt(t, setup.Secret, 1)}, pending, sa.cookie)
	session := cookieNamed(cookies, "rcd_session")
	if status != http.StatusOK || session == nil || session.Value == "" {
		t.Fatalf("second step = %d %s", status, body)
	}
	if code := sa.call(http.MethodGet, "/auth/session", nil, nil); code != http.StatusUnauthorized {
		t.Errorf("session sent along after the code = %d, want 401 (D-052)", code)
	}
	fresh := &client{t: t, base: s.base, cookie: session}
	fresh.must(http.MethodGet, "/auth/session", nil, nil, http.StatusOK)

	// The admin sees only on/off and the address flag, resets, the
	// sessions end.
	var list struct {
		Items []struct {
			ID          string `json:"id"`
			Pseudonym   string `json:"pseudonym"`
			TOTPEnabled bool   `json:"totp_enabled"`
			EmailSet    bool   `json:"email_set"`
		}
	}
	admin := s.users["admin"]
	admin.must(http.MethodGet, "/users", nil, &list, http.StatusOK)
	id := ""
	for _, u := range list.Items {
		if u.Pseudonym == "sa1" {
			id = u.ID
			if !u.TOTPEnabled || u.EmailSet {
				t.Errorf("admin view = %+v", u)
			}
		}
	}
	admin.must(http.MethodDelete, "/users/"+id+"/second-factor", nil, nil, http.StatusNoContent)
	if code := fresh.call(http.MethodGet, "/auth/session", nil, nil); code != http.StatusUnauthorized {
		t.Errorf("session after the reset = %d", code)
	}
	status, _, _ = post(t, s.base, "/auth/login", map[string]string{"pseudonym": "sa1", "password": "password of sa1"})
	if status != http.StatusOK {
		t.Errorf("login after the reset = %d, want one step", status)
	}
}

// TestRequiredSecondFactorRestricts covers auth.totp_required: a session
// without a second factor reaches only the setup, gets 403
// totp_setup_required elsewhere and no WebSocket; after the setup it is
// free.
func TestRequiredSecondFactorRestricts(t *testing.T) {
	game, err := rcontest.Start("rconpw", rcontest.Minecraft, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(game.Close)
	s := newStack(t, game.Addr, "rconpw")
	admin := s.users["admin"]
	admin.must(http.MethodPatch, "/settings", map[string]any{"auth.totp_required": true}, nil, http.StatusOK)

	sa := login(t, s.base, "sa1", "password of sa1")
	var sess struct {
		Required bool `json:"totp_setup_required"`
	}
	sa.must(http.MethodGet, "/auth/session", nil, &sess, http.StatusOK)
	if !sess.Required {
		t.Fatal("session not restricted")
	}
	status, reason := problemOf(t, sa, http.MethodGet, "/servers", nil)
	if status != http.StatusForbidden || reason != "totp_setup_required" {
		t.Errorf("servers = %d %s", status, reason)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(s.base, "http")+"/api/v1/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {s.base}, "Cookie": {sa.cookie.String()}},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("WebSocket of a restricted session: %v", err)
	}
	var setup struct{ Secret string }
	sa.must(http.MethodPost, "/me/second-factor/setup", map[string]string{"password": "password of sa1"}, &setup, http.StatusOK)
	sa.must(http.MethodPost, "/me/second-factor/confirm", map[string]string{"code": codeAt(t, setup.Secret, 0)}, nil, http.StatusNoContent)
	sa.must(http.MethodGet, "/servers", nil, nil, http.StatusOK)
	// Switching off is refused while it is required.
	if code := sa.call(http.MethodPost, "/me/second-factor/disable", map[string]string{"password": "password of sa1", "code": codeAt(t, setup.Secret, 1)}, nil); code != http.StatusConflict {
		t.Errorf("off while required = %d", code)
	}
}
