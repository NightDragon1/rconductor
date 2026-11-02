// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import "net/http"

// APIPolicy is the Content-Security-Policy of every answer that is not the
// interface: nothing may load or frame it (SEC-009).
const APIPolicy = "default-src 'none'; frame-ancestors 'none'"

// withSecurityHeaders sets the headers every answer carries (SEC-009).
// The interface replaces the policy with its own (internal/webui); other
// handlers may set a stricter one.
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", APIPolicy)
		next.ServeHTTP(w, r)
	})
}
