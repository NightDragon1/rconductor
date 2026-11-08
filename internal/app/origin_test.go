// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// originAnswer sends method path with the session cookie (nil: none) and
// the Origin header (empty: none) and reports the status and whether the
// origin check refused it (403 forbidden, "origin not allowed").
func originAnswer(t *testing.T, base, method, path, body, origin string, cookie *http.Cookie) (int, bool) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, base+"/api/v1"+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	var p struct{ Reason, Detail string }
	_ = json.Unmarshal(b, &p)
	return resp.StatusCode, resp.StatusCode == http.StatusForbidden && p.Reason == "forbidden" && p.Detail == "origin not allowed"
}

// TestOriginCheckOnEveryWrite covers the CSRF part of Design 2.1 (AUTH-006):
// every writing operation of api/openapi.yaml is refused by the origin check
// when the Origin header is missing, foreign, "null", or names the panel's
// host with another port or scheme, also with a valid session of the RCON
// admin; with the right origin the origin check lets it through, whatever
// the operation then answers. The login without a session follows the same
// rule.
func TestOriginCheckOnEveryWrite(t *testing.T) {
	f := newRightsFixture(t)
	u, err := url.Parse(f.base)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(u.Port())
	wrong := []string{
		"", "null", "http://evil.example",
		"http://" + u.Hostname() + ":" + strconv.Itoa(port+1), // same host, other port
		"https://" + u.Host,                  // same host, other scheme
		"http://" + u.Host + ".evil.example", // the host as a prefix
	}
	session := f.as(rA).cookie

	param := regexp.MustCompile(`\{[^}]+\}`)
	var writes []string
	for _, op := range openAPIOperations(t) {
		method, _, _ := strings.Cut(op, " ")
		switch method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			writes = append(writes, param.ReplaceAllString(op, "0192f3a4-0000-7000-8000-000000000001"))
		}
	}
	if len(writes) < 20 {
		t.Fatalf("only %d writing operations in the API file", len(writes))
	}
	for _, op := range writes {
		method, path, _ := strings.Cut(op, " ")
		for _, origin := range wrong {
			if status, byOrigin := originAnswer(t, f.base, method, path, "{}", origin, session); !byOrigin {
				t.Errorf("%s with origin %q = %d, want 403 origin not allowed", op, origin, status)
			}
		}
		// Logout would end the session for the other operations: last.
		if op == "POST /auth/logout" {
			continue
		}
		if status, byOrigin := originAnswer(t, f.base, method, path, "{}", f.base, session); byOrigin {
			t.Errorf("%s with the right origin = %d, refused by the origin check", op, status)
		}
	}

	login := `{"pseudonym":"` + rMR + `","password":"` + matrixPassword + `"}`
	for _, origin := range wrong {
		if status, byOrigin := originAnswer(t, f.base, http.MethodPost, "/auth/login", login, origin, nil); !byOrigin {
			t.Errorf("login without session, origin %q = %d, want 403 origin not allowed", origin, status)
		}
	}
	if status, _ := originAnswer(t, f.base, http.MethodPost, "/auth/login", login, f.base, nil); status != http.StatusOK {
		t.Errorf("login without session, right origin = %d", status)
	}
	if status, byOrigin := originAnswer(t, f.base, http.MethodPost, "/auth/logout", "", f.base, session); byOrigin || status != http.StatusNoContent {
		t.Errorf("logout with the right origin = %d", status)
	}
}
