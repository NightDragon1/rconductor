// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"net/http"

	"github.com/NightDragon1/rconductor/internal/store"
)

// HeaderRequestID carries the request ID in every response; Problem
// Details repeat it as request_id.
const HeaderRequestID = "X-Request-ID"

type requestIDKey struct{}

// RequestID returns the request ID of ctx, or "" outside a request.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// withRequestID gives every request a new UUIDv7. An ID sent by the client
// is ignored, so log lines and audit entries cannot be forged into
// another request's trail.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := store.NewID()
		if err != nil {
			WriteProblem(w, http.StatusInternalServerError, "", "")
			return
		}
		w.Header().Set(HeaderRequestID, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}
