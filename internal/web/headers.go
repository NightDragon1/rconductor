// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/sync/semaphore"

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

// A body under its limit can still take many times its size when decoded,
// measured up to 57 times, and the rights of an operation are checked after
// that: a session of any role could fill the memory with parallel requests
// (T-025). So only this many bytes of bodies are in work at once, and more wait
// up to bodyWait. Four of the largest bodies fit; everyday ones are far smaller.
const (
	decodeBudget  = 1 << 20
	pictureBudget = 4 * MaxPictureBody
	bodyWait      = 5 * time.Second
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

// limitBodies keeps the bodies in work within their budgets. It reads a JSON
// body first, so that a slow sender holds no part of the budget; a picture
// is too large for that and takes its part before it is read. Operations open
// to anyone stay outside: their bodies are small, and without a session nobody
// must be able to use up what the signed-in users need. The generated code
// applies middleware per request, so the budgets live in the server.
func (s *apiServer) limitBodies(next http.Handler) http.Handler {
	take := func(w http.ResponseWriter, r *http.Request, budget *semaphore.Weighted, n int64) bool {
		ctx, cancel := context.WithTimeout(r.Context(), bodyWait)
		defer cancel()
		if err := budget.Acquire(ctx, n); err != nil {
			w.Header().Set("Retry-After", "5")
			WriteProblem(w, http.StatusServiceUnavailable, api.ReasonCodeBusy, "the panel is busy, try again")
			return false
		}
		return true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.ContentLength == 0 || publicBody(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/me/avatar") {
			if take(w, r, s.pictures, MaxPictureBody) {
				defer s.pictures.Release(MaxPictureBody)
				next.ServeHTTP(w, r)
			}
			return
		}
		size := r.ContentLength
		if size < 0 {
			size = MaxBody
		}
		var body bytes.Buffer
		body.Grow(int(size) + bytes.MinRead)
		if _, err := body.ReadFrom(r.Body); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				WriteProblem(w, http.StatusRequestEntityTooLarge, api.ReasonCodeInvalid, "the request is too large")
			} else {
				WriteProblem(w, http.StatusBadRequest, api.ReasonCodeInvalid, "the request could not be read")
			}
			return
		}
		n := int64(body.Len())
		if !take(w, r, s.decode, n) {
			return
		}
		defer s.decode.Release(n)
		r.Body = io.NopCloser(&body)
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
