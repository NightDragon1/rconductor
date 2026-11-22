// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/diag"
	"github.com/NightDragon1/rconductor/internal/users"
)

// GetDiagnostics is the diagnostic export (NFR-018, D-090) behind the
// flag diagnostics_export; internal/diag checks role, period and the
// packages at the same time (503). Without from, the last 24 hours.
func (s *apiServer) GetDiagnostics(ctx context.Context, req api.GetDiagnosticsRequestObject) (api.GetDiagnosticsResponseObject, error) {
	to := time.Now().UTC()
	if req.Params.To != nil {
		to = *req.Params.To
	}
	from := to.Add(-24 * time.Hour)
	if req.Params.From != nil {
		from = *req.Params.From
	}
	zipped, err := s.diag.Export(ctx, adminRequest(ctx), from, to)
	if err == nil {
		return api.GetDiagnostics200ApplicationzipResponse{Body: bytes.NewReader(zipped), ContentLength: int64(len(zipped))}, nil
	}
	if inv := (*users.InvalidError)(nil); errors.As(err, &inv) {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: inv.Detail}
	}
	if errors.Is(err, diag.ErrBusy) {
		return nil, &problem{status: http.StatusServiceUnavailable, reason: api.ReasonCodeBusy, detail: err.Error(), retryAfter: 5}
	}
	return nil, adminError(err)
}

// ListSystemLog is the system log (UI-025, D-091) behind the flag
// diagnostics_export; internal/diag checks role, source and period.
// Without from, the last hour.
func (s *apiServer) ListSystemLog(ctx context.Context, req api.ListSystemLogRequestObject) (api.ListSystemLogResponseObject, error) {
	// limit, level and after are checked by internal/diag after the role
	// (D-051): a refused role is 403 and audited whatever it sent.
	p := req.Params
	q := diag.LogQuery{Source: "panel", To: time.Now().UTC(), Level: "info", Limit: 200}
	if p.Source != nil {
		q.Source = *p.Source
	}
	if p.To != nil {
		q.To = *p.To
	}
	q.From = q.To.Add(-time.Hour)
	if p.From != nil {
		q.From = *p.From
	}
	if p.Level != nil {
		q.Level = string(*p.Level)
	}
	if p.After != nil {
		q.After = *p.After
		if q.After < 1 {
			q.After = -1 // given and below 1: refused by the service
		}
	}
	if p.Limit != nil {
		q.Limit = *p.Limit
	}
	page, err := s.diag.SystemLog(ctx, adminRequest(ctx), q)
	inv := (*users.InvalidError)(nil)
	switch {
	case err == nil:
	case errors.Is(err, diag.ErrAgentUnavailable):
		return nil, &problem{status: http.StatusServiceUnavailable, reason: api.ReasonCodeAgentUnavailable, detail: err.Error()}
	case errors.Is(err, diag.ErrAgentBusy):
		return nil, &problem{status: http.StatusServiceUnavailable, reason: api.ReasonCodeBusy, detail: err.Error(), retryAfter: 10}
	case errors.Is(err, diag.ErrAgentUnsupported):
		return nil, &problem{status: http.StatusConflict, reason: api.ReasonCodeAgentUnsupported, detail: err.Error()}
	case errors.Is(err, diag.ErrAgentFailed):
		return nil, &problem{status: http.StatusBadGateway, reason: api.ReasonCodeActionFailed, detail: err.Error()}
	case errors.As(err, &inv):
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: inv.Detail}
	default:
		return nil, adminError(err)
	}
	out := api.ListSystemLog200JSONResponse{Source: page.Source, Items: make([]api.SystemLogEntry, 0, len(page.Items))}
	if page.Truncated {
		out.Truncated = &page.Truncated
	}
	for _, e := range page.Items {
		item := api.SystemLogEntry{Text: e.Text}
		if e.ID > 0 {
			item.Id, item.Time = &e.ID, &e.Time
		}
		if e.Level != "" {
			l := api.SystemLogLevel(e.Level)
			item.Level = &l
		}
		if e.Code != "" {
			item.Code = &e.Code
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}
