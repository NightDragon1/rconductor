// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/servers"
)

func uuids(ids []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))
	for _, s := range ids {
		if id, err := uuid.Parse(s); err == nil {
			out = append(out, id)
		}
	}
	return out
}

func toAPIServer(s servers.Server) api.Server {
	id, _ := uuid.Parse(s.ID)
	out := api.Server{
		Id: id, Name: s.Name, ProfileId: uuidPtr(s.ProfileID), Groups: uuids(s.GroupIDs),
		Status: api.ServerStatus(s.Status), StatusSince: s.StatusSince,
	}
	if !s.Detailed {
		return out
	}
	cred := api.ServerCredentials(s.Credentials)
	ports := make([]api.GamePort, 0, len(s.GamePorts))
	for _, p := range s.GamePorts {
		ports = append(ports, api.GamePort{Port: p.Port, Proto: api.GamePortProto(p.Proto), Name: p.Name})
	}
	out.Protocol, out.Host, out.Port, out.Credentials = &s.Protocol, &s.Host, &s.Port, &cred
	out.AgentId, out.ContainerId = strPtr(s.AgentID), strPtr(s.ContainerID)
	out.GamePorts, out.GamePortsOverride = &ports, &s.GamePortsOverride
	return out
}

func serverInput(b *api.ServerInput) servers.Input {
	in := servers.Input{Name: b.Name, Host: b.Host, Port: b.Port}
	if b.ProfileId != nil {
		p := b.ProfileId.String()
		in.ProfileID = &p
	}
	if b.Password != nil {
		pw := logging.Secret(*b.Password)
		in.Password = &pw
	}
	if b.GamePorts != nil {
		ports := make([]profiles.GamePort, 0, len(*b.GamePorts))
		for _, p := range *b.GamePorts {
			ports = append(ports, profiles.GamePort{Port: p.Port, Proto: string(p.Proto), Name: p.Name})
		}
		in.GamePorts = &ports
	}
	return in
}

func bodyRequired() error {
	return &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "body required"}
}

func (s *apiServer) ListServers(ctx context.Context, _ api.ListServersRequestObject) (api.ListServersResponseObject, error) {
	list, err := s.servers.List(ctx, adminRequest(ctx))
	if err != nil {
		return nil, adminError(err)
	}
	out := api.ServerList{Items: make([]api.Server, 0, len(list))}
	for _, sv := range list {
		out.Items = append(out.Items, toAPIServer(sv))
	}
	return api.ListServers200JSONResponse(out), nil
}

func (s *apiServer) CreateServer(ctx context.Context, req api.CreateServerRequestObject) (api.CreateServerResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	sv, err := s.servers.Create(ctx, adminRequest(ctx), serverInput(req.Body))
	if err != nil {
		return nil, adminError(err)
	}
	return api.CreateServer201JSONResponse(toAPIServer(sv)), nil
}

func (s *apiServer) GetServer(ctx context.Context, req api.GetServerRequestObject) (api.GetServerResponseObject, error) {
	sv, err := s.servers.Get(ctx, adminRequest(ctx), req.Id.String())
	if err != nil {
		return nil, adminError(err)
	}
	return api.GetServer200JSONResponse(toAPIServer(sv)), nil
}

func (s *apiServer) UpdateServer(ctx context.Context, req api.UpdateServerRequestObject) (api.UpdateServerResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	sv, err := s.servers.Update(ctx, adminRequest(ctx), req.Id.String(), serverInput(req.Body))
	if err != nil {
		return nil, adminError(err)
	}
	return api.UpdateServer200JSONResponse(toAPIServer(sv)), nil
}

func (s *apiServer) DeleteServer(ctx context.Context, req api.DeleteServerRequestObject) (api.DeleteServerResponseObject, error) {
	if err := s.servers.Delete(ctx, adminRequest(ctx), req.Id.String()); err != nil {
		return nil, adminError(err)
	}
	return api.DeleteServer204Response{}, nil
}

func toAPIServerGroup(g servers.Group) api.ServerGroup {
	id, _ := uuid.Parse(g.ID)
	return api.ServerGroup{Id: id, Name: g.Name, ServerIds: uuids(g.ServerIDs)}
}

func idStrings(in *[]uuid.UUID) *[]string {
	if in == nil {
		return nil
	}
	out := make([]string, 0, len(*in))
	for _, id := range *in {
		out = append(out, id.String())
	}
	return &out
}

func (s *apiServer) ListServerGroups(ctx context.Context, _ api.ListServerGroupsRequestObject) (api.ListServerGroupsResponseObject, error) {
	gs, err := s.servers.Groups(ctx, adminRequest(ctx))
	if err != nil {
		return nil, adminError(err)
	}
	out := api.ServerGroupList{Items: make([]api.ServerGroup, 0, len(gs))}
	for _, g := range gs {
		out.Items = append(out.Items, toAPIServerGroup(g))
	}
	return api.ListServerGroups200JSONResponse(out), nil
}

func (s *apiServer) CreateServerGroup(ctx context.Context, req api.CreateServerGroupRequestObject) (api.CreateServerGroupResponseObject, error) {
	if req.Body == nil || req.Body.Name == nil {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "name required"}
	}
	var members []string
	if m := idStrings(req.Body.ServerIds); m != nil {
		members = *m
	}
	g, err := s.servers.CreateGroup(ctx, adminRequest(ctx), *req.Body.Name, members)
	if err != nil {
		return nil, adminError(err)
	}
	return api.CreateServerGroup201JSONResponse(toAPIServerGroup(g)), nil
}

func (s *apiServer) GetServerGroup(ctx context.Context, req api.GetServerGroupRequestObject) (api.GetServerGroupResponseObject, error) {
	g, err := s.servers.Group(ctx, adminRequest(ctx), req.Id.String())
	if err != nil {
		return nil, adminError(err)
	}
	return api.GetServerGroup200JSONResponse(toAPIServerGroup(g)), nil
}

func (s *apiServer) UpdateServerGroup(ctx context.Context, req api.UpdateServerGroupRequestObject) (api.UpdateServerGroupResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	g, err := s.servers.UpdateGroup(ctx, adminRequest(ctx), req.Id.String(), req.Body.Name, idStrings(req.Body.ServerIds))
	if err != nil {
		return nil, adminError(err)
	}
	return api.UpdateServerGroup200JSONResponse(toAPIServerGroup(g)), nil
}

func (s *apiServer) DeleteServerGroup(ctx context.Context, req api.DeleteServerGroupRequestObject) (api.DeleteServerGroupResponseObject, error) {
	if err := s.servers.DeleteGroup(ctx, adminRequest(ctx), req.Id.String()); err != nil {
		return nil, adminError(err)
	}
	return api.DeleteServerGroup204Response{}, nil
}
