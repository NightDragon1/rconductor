// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/users"
)

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *apiServer) ListAudit(ctx context.Context, req api.ListAuditRequestObject) (api.ListAuditResponseObject, error) {
	q := users.AuditQuery{ServerID: idOf(req.Params.ServerId)}
	if req.Params.Kind != nil {
		q.Kind = string(*req.Params.Kind)
	}
	if req.Params.Limit != nil {
		q.Limit = *req.Params.Limit
	}
	if req.Params.Cursor != nil {
		q.Cursor = *req.Params.Cursor
	}
	entries, next, err := s.users.Audit(ctx, adminRequest(ctx), q)
	if err != nil {
		return nil, adminError(err)
	}
	page := api.AuditPage{Items: make([]api.AuditEntry, 0, len(entries)), NextCursor: strPtr(next)}
	for _, e := range entries {
		id, _ := uuid.Parse(e.ID)
		page.Items = append(page.Items, api.AuditEntry{
			Id: id, Ts: e.TS, Kind: api.AuditEntryKind(e.Kind), ActorId: uuidPtr(e.ActorID), ActorRole: strPtr(e.ActorRole),
			Session: strPtr(e.SessionRef), ServerId: uuidPtr(e.ServerID), TargetType: strPtr(e.TargetType),
			TargetId: strPtr(e.TargetID), Action: e.Action, Payload: e.Payload,
			Result: api.AuditEntryResult(e.Result), Reason: strPtr(e.Reason),
		})
	}
	return api.ListAudit200JSONResponse(page), nil
}

func toAPISettings(in map[string]json.RawMessage) api.Settings {
	out := api.Settings{}
	for k, raw := range in {
		var v any
		_ = json.Unmarshal(raw, &v)
		out[k] = v
	}
	return out
}

func (s *apiServer) GetSettings(ctx context.Context, _ api.GetSettingsRequestObject) (api.GetSettingsResponseObject, error) {
	set, err := s.users.Settings(ctx, adminRequest(ctx))
	if err != nil {
		return nil, adminError(err)
	}
	return api.GetSettings200JSONResponse{Body: toAPISettings(set), Headers: api.GetSettings200ResponseHeaders{ETag: s.settingsETag(ctx)}}, nil
}

func (s *apiServer) UpdateSettings(ctx context.Context, req api.UpdateSettingsRequestObject) (api.UpdateSettingsResponseObject, error) {
	if req.Body == nil {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "body required"}
	}
	in := map[string]json.RawMessage{}
	for k, v := range *req.Body {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "invalid value"}
		}
		in[k] = b
	}
	set, err := s.users.UpdateSettings(ctx, adminRequest(ctx), in, ifMatch(req.Params.IfMatch))
	if err != nil {
		return nil, adminError(err)
	}
	return api.UpdateSettings200JSONResponse{Body: toAPISettings(set), Headers: api.UpdateSettings200ResponseHeaders{ETag: s.settingsETag(ctx)}}, nil
}
