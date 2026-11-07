// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/users"
)

func toAPIGroup(g users.Group) api.UserGroup {
	id, _ := uuid.Parse(g.ID)
	owner, _ := uuid.Parse(g.OwnerID)
	out := api.UserGroup{
		Id: id, Name: g.Name, Kind: api.GroupKind(g.Kind), OwnerId: owner,
		CommandListId: uuidPtr(g.CommandListID), Members: []uuid.UUID{}, Grants: []api.Target{},
	}
	for _, m := range g.Members {
		if p := uuidPtr(m); p != nil {
			out.Members = append(out.Members, *p)
		}
	}
	for _, t := range g.Grants {
		out.Grants = append(out.Grants, api.Target{ServerId: uuidPtr(t.ServerID), ServerGroupId: uuidPtr(t.ServerGroupID)})
	}
	return out
}

func targetsOf(in []api.Target) []users.Target {
	out := make([]users.Target, 0, len(in))
	for _, t := range in {
		out = append(out, users.Target{ServerID: idOf(t.ServerId), ServerGroupID: idOf(t.ServerGroupId)})
	}
	return out
}

func (s *apiServer) ListUserGroups(ctx context.Context, _ api.ListUserGroupsRequestObject) (api.ListUserGroupsResponseObject, error) {
	gs, err := s.users.Groups(ctx, adminRequest(ctx))
	if err != nil {
		return nil, adminError(err)
	}
	out := api.UserGroupList{Items: make([]api.UserGroup, 0, len(gs))}
	for _, g := range gs {
		out.Items = append(out.Items, s.apiGroup(ctx, g))
	}
	return api.ListUserGroups200JSONResponse(out), nil
}

func (s *apiServer) CreateUserGroup(ctx context.Context, req api.CreateUserGroupRequestObject) (api.CreateUserGroupResponseObject, error) {
	if req.Body == nil {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "body required"}
	}
	g, err := s.users.CreateGroup(ctx, adminRequest(ctx), req.Body.Name, string(req.Body.Kind), idOf(req.Body.CommandListId))
	if err != nil {
		return nil, adminError(err)
	}
	return api.CreateUserGroup201JSONResponse(s.apiGroup(ctx, g)), nil
}

func (s *apiServer) GetUserGroup(ctx context.Context, req api.GetUserGroupRequestObject) (api.GetUserGroupResponseObject, error) {
	g, err := s.users.Group(ctx, adminRequest(ctx), req.Id.String())
	if err != nil {
		return nil, adminError(err)
	}
	return api.GetUserGroup200JSONResponse(s.apiGroup(ctx, g)), nil
}

func (s *apiServer) UpdateUserGroup(ctx context.Context, req api.UpdateUserGroupRequestObject) (api.UpdateUserGroupResponseObject, error) {
	if req.Body == nil {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "body required"}
	}
	up := users.GroupUpdate{Name: req.Body.Name, CommandListID: req.Body.CommandListId, Version: ifMatch(req.Params.IfMatch)}
	if req.Body.CommandListId != nil && *req.Body.CommandListId != "" {
		if _, err := uuid.Parse(*req.Body.CommandListId); err != nil {
			return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "command_list_id is not a UUID"}
		}
	}
	if m := req.Body.Members; m != nil {
		v := make([]string, 0, len(*m))
		for _, id := range *m {
			v = append(v, id.String())
		}
		up.Members = &v
	}
	if g := req.Body.Grants; g != nil {
		v := targetsOf(*g)
		up.Grants = &v
	}
	g, err := s.users.UpdateGroup(ctx, adminRequest(ctx), req.Id.String(), up)
	if err != nil {
		return nil, adminError(err)
	}
	return api.UpdateUserGroup200JSONResponse(s.apiGroup(ctx, g)), nil
}

func (s *apiServer) DeleteUserGroup(ctx context.Context, req api.DeleteUserGroupRequestObject) (api.DeleteUserGroupResponseObject, error) {
	if err := s.users.DeleteGroup(ctx, adminRequest(ctx), req.Id.String()); err != nil {
		return nil, adminError(err)
	}
	return api.DeleteUserGroup204Response{}, nil
}

// apiGroup adds the stored version (revision 2.4 candidate 24).
func (s *apiServer) apiGroup(ctx context.Context, g users.Group) api.UserGroup {
	out := toAPIGroup(g)
	out.Version = s.groupVersions(ctx)[g.ID]
	return out
}
