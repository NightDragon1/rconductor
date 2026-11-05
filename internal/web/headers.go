// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"net/http"

	"github.com/NightDragon1/rconductor/internal/api"
)

// MaxBody bounds every request body. The largest legal one is a profile
// picture of 1 MiB; login and the other JSON bodies decode before any
// session check, so without it anyone could fill the memory (T-025).
const MaxBody = 2 << 20

// APIPolicy is the Content-Security-Policy of every answer that is not the
// interface: nothing may load or frame it (SEC-009).
const APIPolicy = "default-src 'none'; frame-ancestors 'none'"

// withBodyLimit refuses a body over MaxBody by its declared length and cuts
// one of unknown length at the limit.
func withBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > MaxBody {
			WriteProblem(w, http.StatusRequestEntityTooLarge, api.ReasonCodeInvalid, "the request is too large")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, MaxBody)
		next.ServeHTTP(w, r)
	})
}

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
