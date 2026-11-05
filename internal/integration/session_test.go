// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// apiUser is one user of the panel in the container.
type apiUser struct {
	t      *testing.T
	base   string
	cookie *http.Cookie
}

// call sends body as JSON, decodes the answer into out and returns the
// response headers; it fails the test on another status than want.
func (u *apiUser) call(method, path string, header http.Header, body, out any, want int) http.Header {
	u.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			u.t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, u.base+"/api/v1"+path, rd)
	if err != nil {
		u.t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", u.base)
	if u.cookie != nil {
		req.AddCookie(u.cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		u.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		u.t.Fatal(err)
	}
	if resp.StatusCode != want {
		u.t.Fatalf("%s %s = %d, want %d: %s", method, path, resp.StatusCode, want, data)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "rcd_session" && c.Value != "" {
			u.cookie = c
		}
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			u.t.Fatalf("%s %s: %v", method, path, err)
		}
	}
	return resp.Header
}

func signIn(t *testing.T, base, pseudonym, password string) *apiUser {
	t.Helper()
	u := &apiUser{t: t, base: base}
	u.call(http.MethodPost, "/auth/login", nil, map[string]string{"pseudonym": pseudonym, "password": password}, nil, http.StatusOK)
	return u
}

// TestRoleChangeEndsSession covers the role change of AUTH-006 against the
// real panel: the old session gets 401 and its open WebSocket closes.
func TestRoleChangeEndsSession(t *testing.T) {
	ctx := context.Background()
	ctr := startPanel(ctx, t, newVolumes(t))
	base, err := ctr.PortEndpoint(ctx, "8080/tcp", "http")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`password: (\S+)`).FindStringSubmatch(logs(ctx, t, ctr))
	if m == nil {
		t.Fatal("no initial password in the log")
	}
	admin := signIn(t, base, "admin", m[1])
	admin.call(http.MethodPost, "/me/password", nil, map[string]string{
		"current_password": m[1], "new_password": "the admin password of this test",
	}, nil, http.StatusNoContent)

	var inv struct{ Token string }
	admin.call(http.MethodPost, "/invites", nil, map[string]string{"role": "moderator"}, &inv, http.StatusCreated)
	var created struct{ ID string }
	const password = "the password of the moderator"
	(&apiUser{t: t, base: base}).call(http.MethodPost, "/invites/redeem", nil, map[string]string{
		"token": inv.Token, "pseudonym": "mod", "password": password,
	}, &created, http.StatusCreated)
	mod := signIn(t, base, "mod", password)
	mod.call(http.MethodGet, "/auth/session", nil, nil, nil, http.StatusOK)

	dial, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ws, resp, err := websocket.Dial(dial, "ws"+strings.TrimPrefix(base, "http")+"/api/v1/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {base}, "Cookie": {mod.cookie.String()}},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ws.CloseNow() }()
	closed := make(chan error, 1)
	go func() {
		for {
			if _, _, err := ws.Read(ctx); err != nil {
				closed <- err
				return
			}
		}
	}()

	version := admin.call(http.MethodGet, "/users/"+created.ID, nil, nil, nil, http.StatusOK).Get("ETag")
	admin.call(http.MethodPatch, "/users/"+created.ID, http.Header{"If-Match": {version}},
		map[string]string{"role": "supporter"}, nil, http.StatusOK)

	mod.call(http.MethodGet, "/auth/session", nil, nil, nil, http.StatusUnauthorized)
	select {
	case err := <-closed:
		if errors.Is(err, context.Canceled) {
			t.Errorf("socket read stopped by the test: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("the WebSocket stayed open after the role change")
	}

	// A new sign-in carries the new role.
	var session struct {
		User struct{ Role string }
	}
	signIn(t, base, "mod", password).call(http.MethodGet, "/auth/session", nil, nil, &session, http.StatusOK)
	if session.User.Role != "supporter" {
		t.Errorf("role after the change = %q", session.User.Role)
	}
}
