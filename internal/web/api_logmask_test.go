// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/features"
)

// TestLogMaskingBehindFlag (UI-022, D-088): 404 with the flag off, 401
// without a session; then only the RCON admin passes (403), 428 without a
// version, 400 for bad input without naming a text, 409 for a stale
// version; the texts never come back, nor through /settings.
func TestLogMaskingBehindFlag(t *testing.T) {
	flags, err := features.New(features.Flag{Name: "log_masking_ui", Operations: []string{"GetLogMasking", "UpdateLogMasking"}})
	if err != nil {
		t.Fatal(err)
	}
	f := newAPIFixtureFeatures(t, Proxies{}, flags)
	admin, mod := f.login(t, "Boss"), f.login(t, "Mod")
	const origin = "http://panel.lan"
	put := func(body string) call {
		return call{method: http.MethodPut, path: "/api/v1/settings/log-masking", body: body, origin: origin, headers: map[string]string{"If-Match": `"0"`}}
	}
	ops := []call{{method: http.MethodGet, path: "/api/v1/settings/log-masking"}, put(`{"enabled":true}`)}
	check := func(want int, who *http.Cookie, calls ...call) {
		t.Helper()
		for _, c := range calls {
			if who != nil {
				c.cookies = []*http.Cookie{who}
			}
			rec := f.do(t, c)
			if rec.Code != want {
				t.Errorf("%s %s %s = %d, want %d", c.method, c.path, c.body, rec.Code, want)
			}
			if strings.Contains(rec.Body.String(), "s3cr3t") {
				t.Errorf("answer names a text: %s", rec.Body.String())
			}
		}
	}
	check(http.StatusNotFound, admin, ops...)
	if got := f.do(t, call{method: http.MethodPatch, path: "/api/v1/settings", cookies: []*http.Cookie{admin}, origin: origin,
		body: `{"features.log_masking_ui":true}`}).Code; got != http.StatusOK {
		t.Fatalf("switch flag on = %d", got)
	}
	check(http.StatusUnauthorized, nil, ops...)
	check(http.StatusOK, admin, ops...)
	noVersion := put(`{"texts":[{"value":"s3c"}]}`)
	noVersion.headers = nil
	check(http.StatusForbidden, mod, append(ops, put(`{"texts":[{"value":"s3c"}]}`), noVersion)...)
	check(http.StatusPreconditionRequired, admin, noVersion)
	check(http.StatusBadRequest, admin,
		put(`{"texts":[{"value":"s3c"}]}`), put(`{"texts":[{"value":"s3cr3t\nx"}]}`),
		put(`{"texts":[{"value":"s3cr3t-1"},{"value":"s3cr3t-2"}],"max_texts":1}`),
		put(`{"texts":[{"value":"s3cr3t-long"}],"max_text_chars":4}`),
		put(`{"max_texts":0}`), put(`{"max_texts":201}`), put(`{"max_text_chars":3}`), put(`{"max_text_chars":1025}`),
		put(`{"budget_ms":49}`), put(`{"budget_ms":2001}`))
	check(http.StatusOK, admin, put(`{"texts":[{"value":"s3cr3t-1","ignore_case":true}],"max_texts":1}`))
	check(http.StatusConflict, admin, put(`{"texts":[]}`))
	// null keeps the switch; the global settings neither show nor take the row.
	check(http.StatusOK, admin, call{method: http.MethodPut, path: "/api/v1/settings/log-masking", body: `{"enabled":null}`, origin: origin,
		headers: map[string]string{"If-Match": `"1"`}})
	if got := f.do(t, call{method: http.MethodPatch, path: "/api/v1/settings", cookies: []*http.Cookie{admin}, origin: origin,
		body: `{"logs.masking":{"enabled":false}}`}).Code; got != http.StatusUnprocessableEntity {
		t.Errorf("PATCH /settings with the row = %d", got)
	}
	for _, path := range []string{"/api/v1/settings/log-masking", "/api/v1/settings"} {
		body := f.do(t, call{method: http.MethodGet, path: path, cookies: []*http.Cookie{admin}}).Body.String()
		if strings.Contains(body, "s3cr3t") || (path == "/api/v1/settings" && strings.Contains(body, "logs.masking")) ||
			(path != "/api/v1/settings" && (!strings.Contains(body, `"text_count":1`) || !strings.Contains(body, `"enabled":true`))) {
			t.Errorf("GET %s: %s", path, body)
		}
	}
	// An unreadable row: 500 without content for reads and partial writes.
	if _, err := f.st.DB.Exec(`UPDATE settings SET value = '{"version":3,"texts":"s3cr3t"}' WHERE key = 'logs.masking'`); err != nil {
		t.Fatal(err)
	}
	check(http.StatusInternalServerError, admin, call{method: http.MethodGet, path: "/api/v1/settings/log-masking"}, put(`{"enabled":false}`))
	var refused int
	if err := f.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE action LIKE 'log_masking.%' AND result = 'denied'`).Scan(&refused); err != nil || refused != 4 {
		t.Errorf("audited refusals = %d (%v), want 4", refused, err)
	}
}
