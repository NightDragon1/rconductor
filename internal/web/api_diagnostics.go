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
