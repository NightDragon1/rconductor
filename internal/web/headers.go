// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import "net/http"

// withSecurityHeaders sets the headers every answer carries (SEC-009, in
// part; the full Content-Security-Policy follows with the interface in M4).
// Handlers may set a stricter policy of their own.
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}
