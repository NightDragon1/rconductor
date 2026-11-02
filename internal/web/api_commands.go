// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"errors"
	"math"
	"net/http"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/commands"
)

// commandError maps refusals of the pipeline to Design 3.5.
func commandError(err error) error {
	var denied *commands.DeniedError
	switch {
	case errors.As(err, &denied):
		switch denied.Reason {
		case commands.ReasonInvalid:
			return &problem{status: http.StatusUnprocessableEntity, reason: api.ReasonCodeInvalid, detail: "empty or too long after cleaning"}
		case commands.ReasonRateLimited:
			return &problem{status: http.StatusTooManyRequests, reason: api.ReasonCodeRateLimited,
				retryAfter: max(1, int(math.Ceil(denied.RetryAfter.Seconds())))}
		default:
			return &problem{status: http.StatusForbidden, reason: api.ReasonCode(denied.Reason)}
		}
	case errors.Is(err, commands.ErrUnavailable):
		return &problem{status: http.StatusServiceUnavailable, reason: api.ReasonCodeServerUnavailable}
	case errors.Is(err, commands.ErrTimeout):
		return &problem{status: http.StatusGatewayTimeout, detail: err.Error()}
	}
	return adminError(err)
}

func (s *apiServer) SendCommand(ctx context.Context, req api.SendCommandRequestObject) (api.SendCommandResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	r, err := s.commands.Send(ctx, adminRequest(ctx), req.Id.String(), req.Body.Command)
	if err != nil {
		return nil, commandError(err)
	}
	id, _ := uuid.Parse(r.RequestID)
	return api.SendCommand200JSONResponse(api.CommandResult{
		RequestId: id, Ts: r.TS, Command: r.Command, Response: r.Response, Truncated: r.Truncated,
	}), nil
}

func (s *apiServer) GetHistory(ctx context.Context, req api.GetHistoryRequestObject) (api.GetHistoryResponseObject, error) {
	limit, cursor := 0, ""
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	if req.Params.Cursor != nil {
		cursor = *req.Params.Cursor
	}
	entries, next, err := s.commands.History(ctx, adminRequest(ctx), req.Id.String(), limit, cursor)
	if err != nil {
		return nil, adminError(err)
	}
	page := api.HistoryPage{Items: make([]api.HistoryEntry, 0, len(entries)), NextCursor: strPtr(next)}
	for _, e := range entries {
		id, _ := uuid.Parse(e.RequestID)
		h := api.HistoryEntry{
			RequestId: id, Ts: e.TS, UserId: uuidPtr(e.UserID), Pseudonym: strPtr(e.Pseudonym), Role: strPtr(e.Role),
			Command: e.Command, Response: strPtr(e.Response), Result: api.HistoryEntryResult(e.Result), Reason: strPtr(e.Reason),
		}
		if e.Truncated {
			h.Truncated = &e.Truncated
		}
		page.Items = append(page.Items, h)
	}
	return api.GetHistory200JSONResponse(page), nil
}
