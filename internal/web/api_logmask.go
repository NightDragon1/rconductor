// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/logmask"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Masking settings (UI-022, D-088), contract first: the skeleton in
// internal/logmask checks role and input and answers 501 otherwise.

func (s *apiServer) GetLogMasking(ctx context.Context, _ api.GetLogMaskingRequestObject) (api.GetLogMaskingResponseObject, error) {
	return nil, maskingError(s.masking.Get(ctx, adminRequest(ctx)), "getLogMasking")
}

func (s *apiServer) UpdateLogMasking(ctx context.Context, req api.UpdateLogMaskingRequestObject) (api.UpdateLogMaskingResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	in := logmask.SettingsInput{IfMatch: ifMatch(req.Params.IfMatch), Enabled: req.Body.Enabled, MaxTexts: req.Body.MaxTexts, TextRunes: req.Body.MaxTextChars}
	if req.Body.Texts != nil {
		in.SetTexts = true
		in.Texts = make([]logmask.Text, 0, len(*req.Body.Texts))
		for _, t := range *req.Body.Texts {
			in.Texts = append(in.Texts, logmask.Text{Value: deref(t.Value), Fold: t.IgnoreCase != nil && *t.IgnoreCase})
		}
	}
	return nil, maskingError(s.masking.Update(ctx, adminRequest(ctx), in), "updateLogMasking")
}

// maskingError maps the answers of the skeleton; invalid input is 400.
func maskingError(err error, operationID string) error {
	if errors.Is(err, logmask.ErrNotImplemented) {
		return notImplemented(operationID)
	}
	if inv := (*users.InvalidError)(nil); errors.As(err, &inv) {
		return &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: inv.Detail}
	}
	return adminError(err)
}
