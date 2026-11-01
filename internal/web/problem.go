// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"encoding/json"
	"net/http"

	"github.com/NightDragon1/rconductor/internal/api"
)

// WriteProblem writes an RFC 9457 Problem Details response.
func WriteProblem(w http.ResponseWriter, status int, reason api.ReasonCode, detail string) {
	p := api.Problem{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
	}
	if reason != "" {
		p.Reason = &reason
	}
	if detail != "" {
		p.Detail = &detail
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}
