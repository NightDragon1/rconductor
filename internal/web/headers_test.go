// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSecurityHeaders: every answer, from the API, the interface or an
// error, carries the headers of SEC-009 (in part).
func TestSecurityHeaders(t *testing.T) {
	ui := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ui")) })
	h := NewHandler(Options{UI: ui})
	for _, target := range []string{"/", "/servers/42", "/api/v1/does-not-exist", "/healthz"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		for name, want := range map[string]string{
			"X-Content-Type-Options":  "nosniff",
			"Referrer-Policy":         "no-referrer",
			"Content-Security-Policy": "frame-ancestors 'none'",
		} {
			if got := rec.Header().Get(name); got != want {
				t.Errorf("%s: %s = %q, want %q", target, name, got, want)
			}
		}
	}
}
