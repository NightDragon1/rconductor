// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/api"
)

// multiPath is the one operation whose body may not carry unknown fields:
// the panel makes the batch ID, a client must not name one (D-069).
const multiPath = APIBase + "/commands/multi"

// strictBodies refuses unknown fields in the body of POST /commands/multi
// before the generated handler decodes it, which would drop them silently.
func strictBodies(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != multiPath {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			WriteProblem(w, http.StatusRequestEntityTooLarge, api.ReasonCodeInvalid, "the request is too large")
			return
		}
		if err != nil {
			WriteProblem(w, http.StatusBadRequest, api.ReasonCodeInvalid, "the request could not be read")
			return
		}
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		var in api.MultiCommandInput
		if err := dec.Decode(&in); err != nil {
			WriteProblem(w, http.StatusBadRequest, api.ReasonCodeInvalid, err.Error())
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r)
	})
}

func (s *apiServer) SendMultiCommand(ctx context.Context, req api.SendMultiCommandRequestObject) (api.SendMultiCommandResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	ids := make([]string, 0, len(req.Body.ServerIds))
	for _, id := range req.Body.ServerIds {
		ids = append(ids, id.String())
	}
	batch, items, err := s.commands.Multi(ctx, adminRequest(ctx), ids, req.Body.Command)
	if err != nil {
		return nil, adminError(err)
	}
	bid, _ := uuid.Parse(batch)
	out := api.MultiCommandResult{BatchId: bid, Items: make([]api.MultiCommandItem, 0, len(items))}
	for _, it := range items {
		sid, _ := uuid.Parse(it.ServerID)
		item := api.MultiCommandItem{ServerId: sid, Result: api.MultiCommandItemResult(it.Result), RequestId: uuidPtr(it.RequestID),
			Detail: strPtr(it.Detail), Command: strPtr(it.Command), Response: strPtr(it.Response)}
		if it.Reason != "" {
			r := api.ReasonCode(it.Reason)
			item.Reason = &r
		}
		if it.Truncated {
			item.Truncated = &it.Truncated
		}
		out.Items = append(out.Items, item)
	}
	return api.SendMultiCommand200JSONResponse(out), nil
}
