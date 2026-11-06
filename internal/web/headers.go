// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"net/http"
	"strings"

	"github.com/NightDragon1/rconductor/internal/api"
)

// Request bodies are bounded (T-025): JSON decodes before any session check
// and takes far more memory than its bytes, so its limit is small; only a
// profile picture of 1 MiB needs more.
const (
	MaxBody        = 256 << 10
	MaxPictureBody = 2 << 20
)

// APIPolicy is the Content-Security-Policy of every answer that is not the
// interface: nothing may load or frame it (SEC-009).
const APIPolicy = "default-src 'none'; frame-ancestors 'none'"

// withBodyLimit refuses a body over the limit by its declared length and cuts
// one of unknown length there.
func withBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(MaxBody)
		if strings.HasSuffix(r.URL.Path, "/me/avatar") {
			limit = MaxPictureBody
		}
		if r.ContentLength > limit {
			WriteProblem(w, http.StatusRequestEntityTooLarge, api.ReasonCodeInvalid, "the request is too large")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
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
