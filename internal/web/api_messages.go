// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/messages"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Messages in the panel (MSG-001 to MSG-003, D-085, D-086): behind the
// session and the flag messages; internal/messages checks role and
// audience.

func (s *apiServer) ListMessages(ctx context.Context, req api.ListMessagesRequestObject) (api.ListMessagesResponseObject, error) {
	limit, before := 50, ""
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	if req.Params.Before != nil {
		before = req.Params.Before.String()
	}
	if limit < 1 || limit > 100 {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "limit is 1 to 100"}
	}
	p, err := s.messages.List(ctx, adminRequest(ctx), limit, before)
	if err != nil {
		return nil, messageError(err)
	}
	out := api.ListMessages200JSONResponse{Items: make([]api.Message, 0, len(p.Items)), Unread: p.Unread}
	for _, m := range p.Items {
		out.Items = append(out.Items, apiMessage(m))
	}
	return out, nil
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
	m, err := s.messages.Send(ctx, adminRequest(ctx), in)
	if err != nil {
		return nil, messageError(err)
	}
	return api.CreateMessage201JSONResponse(apiMessage(m)), nil
}

// apiMessage is a message as the API shows it.
func apiMessage(m messages.Message) api.Message {
	out := api.Message{Audience: api.MessageAudience(m.Audience), CommentCount: m.CommentCount, CreatedAt: m.CreatedAt,
		Read: m.Read, Text: m.Text, Sender: api.MessageAuthor{Pseudonym: m.SenderPseudonym}}
	out.Id, _ = uuid.Parse(m.ID)
	if id, err := uuid.Parse(m.SenderID); err == nil {
		out.Sender.Id = &id
	}
	if m.RecipientCount >= 0 {
		n := m.RecipientCount
		out.RecipientCount = &n
	}
	return out
}

func (s *apiServer) GetMessage(ctx context.Context, req api.GetMessageRequestObject) (api.GetMessageResponseObject, error) {
	m, err := s.messages.Get(ctx, adminRequest(ctx), req.Id.String())
	if err != nil {
		return nil, messageError(err)
	}
	return api.GetMessage200JSONResponse(apiMessage(m)), nil
}

func (s *apiServer) DeleteMessage(ctx context.Context, req api.DeleteMessageRequestObject) (api.DeleteMessageResponseObject, error) {
	if err := s.messages.Delete(ctx, adminRequest(ctx), req.Id.String()); err != nil {
		return nil, messageError(err)
	}
	return api.DeleteMessage204Response{}, nil
}

func (s *apiServer) MarkMessageRead(ctx context.Context, req api.MarkMessageReadRequestObject) (api.MarkMessageReadResponseObject, error) {
	if err := s.messages.MarkRead(ctx, adminRequest(ctx), req.Id.String()); err != nil {
		return nil, messageError(err)
	}
	return api.MarkMessageRead204Response{}, nil
}

func (s *apiServer) ListMessageComments(ctx context.Context, req api.ListMessageCommentsRequestObject) (api.ListMessageCommentsResponseObject, error) {
	limit, after := 50, ""
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	if req.Params.After != nil {
		after = req.Params.After.String()
	}
	if limit < 1 || limit > 100 {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "limit is 1 to 100"}
	}
	list, err := s.messages.Comments(ctx, adminRequest(ctx), req.Id.String(), limit, after)
	if err != nil {
		return nil, messageError(err)
	}
	out := api.ListMessageComments200JSONResponse{Items: make([]api.MessageComment, 0, len(list))}
	for _, c := range list {
		out.Items = append(out.Items, apiComment(c))
	}
	return out, nil
}

func (s *apiServer) CreateMessageComment(ctx context.Context, req api.CreateMessageCommentRequestObject) (api.CreateMessageCommentResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	c, err := s.messages.Comment(ctx, adminRequest(ctx), req.Id.String(), req.Body.Text)
	if err != nil {
		return nil, messageError(err)
	}
	return api.CreateMessageComment201JSONResponse(apiComment(c)), nil
}

func (s *apiServer) DeleteMessageComment(ctx context.Context, req api.DeleteMessageCommentRequestObject) (api.DeleteMessageCommentResponseObject, error) {
	if err := s.messages.DeleteComment(ctx, adminRequest(ctx), req.Id.String(), req.CommentId.String()); err != nil {
		return nil, messageError(err)
	}
	return api.DeleteMessageComment204Response{}, nil
}

// apiComment is a comment as the API shows it.
func apiComment(c messages.Comment) api.MessageComment {
	out := api.MessageComment{Text: c.Text, CreatedAt: c.CreatedAt, Author: api.MessageAuthor{Pseudonym: c.AuthorPseudonym}}
	out.Id, _ = uuid.Parse(c.ID)
	if id, err := uuid.Parse(c.AuthorID); err == nil {
		out.Author.Id = &id
	}
	return out
}

// messageError maps the answers of the service; invalid input is 400.
func messageError(err error) error {
	if errors.Is(err, messages.ErrTooManyRecipients) {
		return &problem{status: http.StatusBadRequest, reason: api.ReasonCodeTooManyRecipients, detail: err.Error()}
	}
	if errors.Is(err, messages.ErrRateLimited) {
		return &problem{status: http.StatusTooManyRequests, reason: api.ReasonCodeRateLimited, detail: err.Error(), retryAfter: 60}
	}
	if inv := (*users.InvalidError)(nil); errors.As(err, &inv) {
		return &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: inv.Detail}
	}
	return adminError(err)
}
