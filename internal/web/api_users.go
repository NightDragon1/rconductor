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
	case errors.Is(err, users.ErrConflict):
		return &problem{status: http.StatusConflict, reason: api.ReasonCodeConflict, detail: "changed by someone else since it was read"}
	case errors.Is(err, users.ErrPreconditionRequired):
		return &problem{status: http.StatusPreconditionRequired, reason: api.ReasonCodePreconditionRequired, detail: "If-Match with the version read before is required"}
	case errors.Is(err, users.ErrForbidden):
		return &problem{status: http.StatusForbidden, reason: api.ReasonCodeForbidden, detail: err.Error()}
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
	u.SupportersAllowed = req.Body.SupportersAllowed
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

func (s *apiServer) EndUserSessions(ctx context.Context, req api.EndUserSessionsRequestObject) (api.EndUserSessionsResponseObject, error) {
	if err := s.users.EndSessions(ctx, adminRequest(ctx), req.Id.String()); err != nil {
		return nil, adminError(err)
	}
	return api.EndUserSessions204Response{}, nil
}

func uuidPtr(s string) *uuid.UUID {
	if s == "" {
		return nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	return &id
}

func idOf(p *uuid.UUID) string {
	if p == nil {
		return ""
	}
	return p.String()
}

func toAPIAssignments(a users.Assignments) api.Assignments {
	out := api.Assignments{
		Role: api.Role(a.Role), AdminScopes: []api.Target{}, Groups: []uuid.UUID{},
		Grants: []api.Grant{}, SupporterGrants: []api.SupporterGrant{},
	}
	for _, t := range a.AdminScopes {
		out.AdminScopes = append(out.AdminScopes, api.Target{ServerId: uuidPtr(t.ServerID), ServerGroupId: uuidPtr(t.ServerGroupID)})
	}
	for _, g := range a.Groups {
		if id := uuidPtr(g); id != nil {
			out.Groups = append(out.Groups, *id)
		}
	}
	for _, g := range a.Grants {
		gid, _ := uuid.Parse(g.GroupID)
		out.Grants = append(out.Grants, api.Grant{GroupId: gid, ServerId: uuidPtr(g.ServerID), ServerGroupId: uuidPtr(g.ServerGroupID)})
	}
	for _, g := range a.SupporterGrants {
		m, _ := uuid.Parse(g.ModeratorID)
		sv, _ := uuid.Parse(g.ServerID)
		out.SupporterGrants = append(out.SupporterGrants, api.SupporterGrant{ModeratorId: m, ServerId: sv})
	}
	return out
}

func (s *apiServer) GetAssignments(ctx context.Context, req api.GetAssignmentsRequestObject) (api.GetAssignmentsResponseObject, error) {
	a, err := s.users.Assignments(ctx, adminRequest(ctx), req.Id.String())
	if err != nil {
		return nil, adminError(err)
	}
	return api.GetAssignments200JSONResponse(toAPIAssignments(a)), nil
}

func (s *apiServer) UpdateAssignments(ctx context.Context, req api.UpdateAssignmentsRequestObject) (api.UpdateAssignmentsResponseObject, error) {
	if req.Body == nil {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "body required"}
	}
	var up users.AssignmentsUpdate
	if b := req.Body.AdminScopes; b != nil {
		v := make([]users.Target, 0, len(*b))
		for _, t := range *b {
			v = append(v, users.Target{ServerID: idOf(t.ServerId), ServerGroupID: idOf(t.ServerGroupId)})
		}
		up.AdminScopes = &v
	}
	if b := req.Body.Groups; b != nil {
		v := make([]string, 0, len(*b))
		for _, g := range *b {
			v = append(v, g.String())
		}
		up.Groups = &v
	}
	if b := req.Body.Grants; b != nil {
		v := make([]users.Grant, 0, len(*b))
		for _, g := range *b {
			v = append(v, users.Grant{GroupID: g.GroupId.String(), Target: users.Target{ServerID: idOf(g.ServerId), ServerGroupID: idOf(g.ServerGroupId)}})
		}
		up.Grants = &v
	}
	if b := req.Body.SupporterGrants; b != nil {
		v := make([]users.SupporterGrant, 0, len(*b))
		for _, g := range *b {
			v = append(v, users.SupporterGrant{ModeratorID: g.ModeratorId.String(), ServerID: g.ServerId.String()})
		}
		up.SupporterGrants = &v
	}
	a, err := s.users.UpdateAssignments(ctx, adminRequest(ctx), req.Id.String(), up)
	if err != nil {
		return nil, adminError(err)
	}
	return api.UpdateAssignments200JSONResponse(toAPIAssignments(a)), nil
}
