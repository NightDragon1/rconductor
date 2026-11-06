// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"net/http"
	"strings"

	"github.com/NightDragon1/rconductor/internal/api"
)

// Request bodies are bounded (T-025): JSON takes far more memory than its
// bytes when decoded, so its limit is small, and smaller still for the
// operations open to anyone. Only a profile picture of 1 MiB needs more.
const (
	MaxBody        = 256 << 10
	MaxPublicBody  = 16 << 10
	MaxPictureBody = 2 << 20
)

// publicBody reports whether path takes a body without a session.
func publicBody(path string) bool {
	return strings.HasSuffix(path, "/auth/login") || strings.HasSuffix(path, "/invites/redeem")
}

// APIPolicy is the Content-Security-Policy of every answer that is not the
// interface: nothing may load or frame it (SEC-009).
const APIPolicy = "default-src 'none'; frame-ancestors 'none'"

// withBodyLimit refuses a body over the limit by its declared length and cuts
// one of unknown length there.
func withBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(MaxBody)
		switch {
		case publicBody(r.URL.Path):
			limit = MaxPublicBody
		case strings.HasSuffix(r.URL.Path, "/me/avatar"):
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
