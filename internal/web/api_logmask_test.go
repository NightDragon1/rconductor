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
// without a session; then only the RCON admin passes (403), the input is
// checked (400) without naming a text, and the rest is 501.
func TestLogMaskingBehindFlag(t *testing.T) {
	flags, err := features.New(features.Flag{Name: "log_masking_ui", Operations: []string{"GetLogMasking", "UpdateLogMasking"}})
	if err != nil {
		t.Fatal(err)
	}
	f := newAPIFixtureFeatures(t, Proxies{}, flags)
	admin, mod := f.login(t, "Boss"), f.login(t, "Mod")
	const origin = "http://panel.lan"
	put := func(body string) call {
		return call{method: http.MethodPut, path: "/api/v1/settings/log-masking", body: body, origin: origin, headers: map[string]string{"If-Match": `"1"`}}
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
	check(http.StatusNotImplemented, admin, append(ops, put(`{"texts":[{"value":"s3cr3t-1","ignore_case":true}],"max_texts":1}`), put(`{"texts":[]}`))...)
	check(http.StatusForbidden, mod, append(ops, put(`{"texts":[{"value":"s3c"}]}`))...)
	check(http.StatusBadRequest, admin,
		put(`{"texts":[{"value":"s3c"}]}`), put(`{"texts":[{"value":"s3cr3t\nx"}]}`),
		put(`{"texts":[{"value":"s3cr3t-1"},{"value":"s3cr3t-2"}],"max_texts":1}`),
		put(`{"texts":[{"value":"s3cr3t-long"}],"max_text_chars":4}`),
		put(`{"max_texts":0}`), put(`{"max_texts":201}`), put(`{"max_text_chars":3}`), put(`{"max_text_chars":1025}`))
	var refused int
	if err := f.st.DB.QueryRow(`SELECT count(*) FROM audit_log WHERE action LIKE 'log_masking.%' AND result = 'denied'`).Scan(&refused); err != nil || refused != 3 {
		t.Errorf("audited refusals = %d (%v), want 3", refused, err)
	}
}
