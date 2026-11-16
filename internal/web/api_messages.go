// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/messages"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Messages in the panel (MSG-001 to MSG-003, D-085, D-086): contract
// first; behind the session and the flag messages, the skeleton in
// internal/messages checks role and audience and answers 501 for the rest
// until the backend is built.

func (s *apiServer) ListMessages(ctx context.Context, _ api.ListMessagesRequestObject) (api.ListMessagesResponseObject, error) {
	return nil, messageError(s.messages.List(ctx, adminRequest(ctx)), "listMessages")
}

func (s *apiServer) CreateMessage(ctx context.Context, req api.CreateMessageRequestObject) (api.CreateMessageResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	in := messages.Input{Text: req.Body.Text, Audience: string(req.Body.Audience)}
	if req.Body.UserIds != nil {
		in.UserIDs = make([]string, 0, len(*req.Body.UserIds))
		for _, id := range *req.Body.UserIds {
			in.UserIDs = append(in.UserIDs, id.String())
		}
	}
	return nil, messageError(s.messages.Send(ctx, adminRequest(ctx), in), "createMessage")
}

func (s *apiServer) GetMessage(ctx context.Context, req api.GetMessageRequestObject) (api.GetMessageResponseObject, error) {
	return nil, messageError(s.messages.Get(ctx, adminRequest(ctx), req.Id.String()), "getMessage")
}

func (s *apiServer) DeleteMessage(ctx context.Context, req api.DeleteMessageRequestObject) (api.DeleteMessageResponseObject, error) {
	return nil, messageError(s.messages.Delete(ctx, adminRequest(ctx), req.Id.String()), "deleteMessage")
}

func (s *apiServer) MarkMessageRead(ctx context.Context, req api.MarkMessageReadRequestObject) (api.MarkMessageReadResponseObject, error) {
	return nil, messageError(s.messages.MarkRead(ctx, adminRequest(ctx), req.Id.String()), "markMessageRead")
}

// messageError maps the answers of the skeleton; invalid input is 400.
func messageError(err error, operationID string) error {
	if errors.Is(err, messages.ErrNotImplemented) {
		return notImplemented(operationID)
	}
	if inv := (*users.InvalidError)(nil); errors.As(err, &inv) {
		return &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: inv.Detail}
	}
	return adminError(err)
}
