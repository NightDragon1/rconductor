// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/diag"
	"github.com/NightDragon1/rconductor/internal/users"
)

// GetDiagnostics is the diagnostic export (NFR-018, D-090) behind the
// flag diagnostics_export; internal/diag checks role and period. Without
// from, the last 24 hours.
func (s *apiServer) GetDiagnostics(ctx context.Context, req api.GetDiagnosticsRequestObject) (api.GetDiagnosticsResponseObject, error) {
	to := time.Now().UTC()
	if req.Params.To != nil {
		to = *req.Params.To
	}
	from := to.Add(-24 * time.Hour)
	if req.Params.From != nil {
		from = *req.Params.From
	}
	err := s.diag.Export(ctx, adminRequest(ctx), from, to)
	if errors.Is(err, diag.ErrNotImplemented) {
		return nil, notImplemented("getDiagnostics")
	}
	if inv := (*users.InvalidError)(nil); errors.As(err, &inv) {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: inv.Detail}
	}
	return nil, adminError(err)
}

// ListSystemLog is the system log (UI-025, D-091) behind the flag
// diagnostics_export; internal/diag checks role, source and period.
// Without from, the last hour.
func (s *apiServer) ListSystemLog(ctx context.Context, req api.ListSystemLogRequestObject) (api.ListSystemLogResponseObject, error) {
	p := req.Params
	switch {
	case p.Limit != nil && (*p.Limit < 1 || *p.Limit > 500):
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "limit is 1 to 500"}
	case p.Level != nil && !p.Level.Valid():
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "level is info, warn or error"}
	case p.After != nil && *p.After < 1:
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "after is at least 1"}
	}
	q := diag.LogQuery{Source: "panel", To: time.Now().UTC()}
	if req.Params.Source != nil {
		q.Source = *req.Params.Source
	}
	if req.Params.To != nil {
		q.To = *req.Params.To
	}
	q.From = q.To.Add(-time.Hour)
	if req.Params.From != nil {
		q.From = *req.Params.From
	}
	err := s.diag.SystemLog(ctx, adminRequest(ctx), q)
	if errors.Is(err, diag.ErrNotImplemented) {
		return nil, notImplemented("listSystemLog")
	}
	if inv := (*users.InvalidError)(nil); errors.As(err, &inv) {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: inv.Detail}
	}
	return nil, adminError(err)
}
