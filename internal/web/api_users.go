// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/users"
)

// adminRequest describes the caller for the administration modules.
func adminRequest(ctx context.Context) users.Request {
	p, _ := principal(ctx)
	return users.Request{
		Actor: perm.Actor{ID: p.UserID, Role: perm.Role(p.Role)}, Pseudonym: p.Pseudonym,
		SessionRef: p.SessionRef(), SessionID: p.SessionHash, RequestID: RequestID(ctx),
	}
}

// adminError maps errors of the administration modules to Problem Details.
func adminError(err error) error {
	var inv *users.InvalidError
	switch {
	case errors.Is(err, users.ErrNotFound):
		return &problem{status: http.StatusNotFound, reason: api.ReasonCodeNotFound}
	case errors.Is(err, users.ErrForbidden):
		return &problem{status: http.StatusForbidden, detail: err.Error()}
	case errors.As(err, &inv):
		return &problem{status: http.StatusUnprocessableEntity, reason: api.ReasonCodeInvalid, detail: inv.Detail}
	}
	return err
}

func toAPIUser(u users.User) api.User {
	id, _ := uuid.Parse(u.ID)
	out := api.User{
		Id: id, Pseudonym: u.Pseudonym, Role: api.Role(u.Role), Contacts: u.Contacts, HasAvatar: u.HasAvatar,
		Disabled: u.Disabled, IsEmergency: u.IsEmergency, SupportersAllowed: u.SupportersAllowed,
		CreatedAt: u.CreatedAt, LastLoginAt: u.LastLoginAt,
	}
	if u.RealName != "" {
		out.RealName = &u.RealName
	}
	return out
}

func (s *apiServer) ListUsers(ctx context.Context, req api.ListUsersRequestObject) (api.ListUsersResponseObject, error) {
	var f users.Filter
	if req.Params.Role != nil {
		f.Role = perm.Role(*req.Params.Role)
	}
	if req.Params.Q != nil {
		f.Query = *req.Params.Q
	}
	list, err := s.users.List(ctx, adminRequest(ctx), f)
	if err != nil {
		return nil, adminError(err)
	}
	out := api.UserList{Items: make([]api.User, 0, len(list))}
	for _, u := range list {
		out.Items = append(out.Items, toAPIUser(u))
	}
	return api.ListUsers200JSONResponse(out), nil
}

func (s *apiServer) GetUser(ctx context.Context, req api.GetUserRequestObject) (api.GetUserResponseObject, error) {
	u, err := s.users.Get(ctx, adminRequest(ctx), req.Id.String())
	if err != nil {
		return nil, adminError(err)
	}
	return api.GetUser200JSONResponse(toAPIUser(u)), nil
}

func (s *apiServer) UpdateUser(ctx context.Context, req api.UpdateUserRequestObject) (api.UpdateUserResponseObject, error) {
	if req.Body == nil {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "body required"}
	}
	var u users.Update
	if req.Body.Role != nil {
		r := perm.Role(*req.Body.Role)
		u.Role = &r
	}
	u.Disabled = req.Body.Disabled
	got, err := s.users.Update(ctx, adminRequest(ctx), req.Id.String(), u)
	if err != nil {
		return nil, adminError(err)
	}
	return api.UpdateUser200JSONResponse(toAPIUser(got)), nil
}

func (s *apiServer) DeleteUser(ctx context.Context, req api.DeleteUserRequestObject) (api.DeleteUserResponseObject, error) {
	if err := s.users.Delete(ctx, adminRequest(ctx), req.Id.String()); err != nil {
		return nil, adminError(err)
	}
	return api.DeleteUser204Response{}, nil
}
