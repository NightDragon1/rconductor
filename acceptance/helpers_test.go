// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package acceptance

// Shared helpers for the acceptance tests: one client per user, the admin login
// with the forced password change (AUTH-010), Problem details and TOTP codes.
// Only the standard library; only HTTP against the instance.

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// adminNewPassword replaces the initial password at the first login.
const adminNewPassword = "Acct-Admin-Password-2026!"

func baseURL(t *testing.T) string {
	t.Helper()
	u := strings.TrimRight(os.Getenv("RCD_ACCEPTANCE_URL"), "/")
	if u == "" {
		t.Fatal("RCD_ACCEPTANCE_URL is not set")
	}
	return u
}

type client struct {
	t    *testing.T
	base string
	http *http.Client
}

func newClient(t *testing.T) *client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &client{t: t, base: baseURL(t), http: &http.Client{Jar: jar, Timeout: 60 * time.Second}}
}

type response struct {
	status int
	header http.Header
	body   []byte
}

// do sends a request to /api/v1+path. Writes carry the Origin of the panel
// (CSRF check, Design 2.1). Extra headers come as name, value pairs.
func (c *client) do(method, path string, body any, headers ...string) response {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+"/api/v1"+path, rd)
	if err != nil {
		c.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("Origin", c.base)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return response{status: resp.StatusCode, header: resp.Header, body: b}
}

// want fails the test unless the status matches.
func (r response) want(t *testing.T, what string, status int) response {
	t.Helper()
	if r.status != status {
		t.Fatalf("%s: status %d, want %d; body %s", what, r.status, status, trim(r.body))
	}
	return r
}

func (r response) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decode %s: %v", trim(r.body), err)
	}
}

func (r response) reason(t *testing.T) string {
	t.Helper()
	var p struct {
		Reason string `json:"reason"`
	}
	r.decode(t, &p)
	return p.Reason
}

func trim(b []byte) string {
	if len(b) > 400 {
		return string(b[:400]) + "…"
	}
	return string(b)
}

// login logs a user in and fails unless it gives a session.
func (c *client) login(pseudonym, password string) map[string]any {
	c.t.Helper()
	r := c.do(http.MethodPost, "/auth/login", map[string]string{"pseudonym": pseudonym, "password": password})
	r.want(c.t, "login "+pseudonym, http.StatusOK)
	var s map[string]any
	r.decode(c.t, &s)
	return s
}

var (
	adminOnce   sync.Once
	adminClient *client
	adminErr    string
)

// admin returns the one session of the admin for all tests. The first login
// uses the initial password and sets adminNewPassword (AUTH-010); the
// session stays for all later tests, so a second factor switched on by a
// test needs no code at login.
func admin(t *testing.T) *client {
	t.Helper()
	adminOnce.Do(func() {
		c := newClient(t)
		pw := os.Getenv("RCD_ACCEPTANCE_ADMIN_PASSWORD")
		r := c.do(http.MethodPost, "/auth/login", map[string]string{"pseudonym": "admin", "password": pw})
		if r.status != http.StatusOK {
			r = c.do(http.MethodPost, "/auth/login", map[string]string{"pseudonym": "admin", "password": adminNewPassword})
		}
		if r.status != http.StatusOK {
			adminErr = fmt.Sprintf("admin login: status %d, body %s", r.status, trim(r.body))
			return
		}
		var s struct {
			MustChange bool `json:"must_change_password"`
		}
		_ = json.Unmarshal(r.body, &s)
		if s.MustChange {
			p := c.do(http.MethodPost, "/me/password", map[string]string{"current_password": pw, "new_password": adminNewPassword})
			if p.status != http.StatusNoContent {
				adminErr = fmt.Sprintf("password change: status %d, body %s", p.status, trim(p.body))
				return
			}
		}
		adminClient = c
	})
	if adminClient == nil {
		t.Fatal(adminErr)
	}
	adminClient.t = t
	return adminClient
}

// totp computes the code of RFC 6238 (SHA-1, 30 s, 6 digits).
func totp(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	s := strings.ToUpper(strings.ReplaceAll(secret, " ", ""))
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.TrimRight(s, "="))
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(at.Unix()/30))
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	h := m.Sum(nil)
	o := h[len(h)-1] & 0x0f
	v := binary.BigEndian.Uint32(h[o:o+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1000000)
}

// settings reads all global settings and their ETag.
func settings(t *testing.T, c *client) (map[string]any, string) {
	t.Helper()
	r := c.do(http.MethodGet, "/settings", nil).want(t, "GET /settings", http.StatusOK)
	var m map[string]any
	r.decode(t, &m)
	return m, r.header.Get("ETag")
}

// invite creates an invited user with the given role through c and logs him in.
func invite(t *testing.T, c *client, role, pseudonym, password string) (*client, string) {
	t.Helper()
	r := c.do(http.MethodPost, "/invites", map[string]string{"role": role}).want(t, "invite "+role, http.StatusCreated)
	var inv struct {
		Token string `json:"token"`
	}
	r.decode(t, &inv)
	u := newClient(t)
	r = u.do(http.MethodPost, "/invites/redeem", map[string]string{"token": inv.Token, "pseudonym": pseudonym, "password": password}).
		want(t, "redeem "+pseudonym, http.StatusCreated)
	var su struct {
		ID string `json:"id"`
	}
	r.decode(t, &su)
	u.login(pseudonym, password)
	return u, su.ID
}
