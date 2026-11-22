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

// Masking settings (UI-022, D-088 c): RCON admin only; the texts are
// never returned, only their number.

func toAPILogMasking(st logmask.State) api.LogMasking {
	return api.LogMasking{Enabled: st.Enabled, TextCount: st.TextCount, MaxTexts: st.MaxTexts, MaxTextChars: st.TextRunes,
		BudgetMs: st.BudgetMS, Version: versionString(st.Version)}
}

func (s *apiServer) GetLogMasking(ctx context.Context, _ api.GetLogMaskingRequestObject) (api.GetLogMaskingResponseObject, error) {
	st, err := s.masking.Read(ctx, adminRequest(ctx))
	if err != nil {
		return nil, maskingError(err)
	}
	return api.GetLogMasking200JSONResponse(toAPILogMasking(st)), nil
}

func (s *apiServer) UpdateLogMasking(ctx context.Context, req api.UpdateLogMaskingRequestObject) (api.UpdateLogMaskingResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	in := logmask.SettingsInput{IfMatch: ifMatch(req.Params.IfMatch), Enabled: req.Body.Enabled, MaxTexts: req.Body.MaxTexts,
		TextRunes: req.Body.MaxTextChars, BudgetMS: req.Body.BudgetMs}
	if req.Body.Texts != nil {
		in.SetTexts = true
		in.Texts = make([]logmask.Text, 0, len(*req.Body.Texts))
		for _, t := range *req.Body.Texts {
			in.Texts = append(in.Texts, logmask.NewText(deref(t.Value), t.IgnoreCase != nil && *t.IgnoreCase))
		}
	}
	st, err := s.masking.Write(ctx, adminRequest(ctx), in)
	if err != nil {
		return nil, maskingError(err)
	}
	return api.UpdateLogMasking200JSONResponse(toAPILogMasking(st)), nil
}

// maskingError maps the answers of the settings; invalid input is 400.
func maskingError(err error) error {
	if inv := (*users.InvalidError)(nil); errors.As(err, &inv) {
		return &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: inv.Detail}
	}
	return adminError(err)
}
