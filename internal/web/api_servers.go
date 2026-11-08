// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/agentview"
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
	out.AgentId, out.ContainerName = strPtr(s.AgentID), strPtr(s.ContainerName)
	out.GamePorts, out.GamePortsOverride = &ports, &s.GamePortsOverride
	return out
}

func serverInput(b *api.ServerInput) servers.Input {
	in := servers.Input{Name: b.Name, Host: b.Host, Port: b.Port, AgentID: b.AgentId, ContainerName: b.ContainerName}
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
		out.Items = append(out.Items, s.withRuntime(ctx, sv))
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
	return api.CreateServer201JSONResponse(s.withRuntime(ctx, sv)), nil
}

func (s *apiServer) TestServerConnection(ctx context.Context, req api.TestServerConnectionRequestObject) (api.TestServerConnectionResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	in := servers.ConnectionData{Host: req.Body.Host, Port: req.Body.Port}
	if id := req.Body.ServerId; id != nil {
		in.ServerID = id.String()
	}
	if id := req.Body.ProfileId; id != nil {
		v := id.String()
		in.ProfileID = &v
	}
	if pw := req.Body.Password; pw != nil {
		v := logging.Secret(*pw)
		in.Password = &v
	}
	result, err := s.servers.TestConnection(ctx, adminRequest(ctx), in)
	if errors.Is(err, servers.ErrTestRateLimited) {
		return nil, &problem{status: http.StatusTooManyRequests, reason: api.ReasonCodeRateLimited, retryAfter: 6}
	}
	if err != nil {
		return nil, adminError(err)
	}
	return api.TestServerConnection200JSONResponse{Result: api.ConnectionTestResultResult(result)}, nil
}

func (s *apiServer) GetServer(ctx context.Context, req api.GetServerRequestObject) (api.GetServerResponseObject, error) {
	sv, err := s.servers.Get(ctx, adminRequest(ctx), req.Id.String())
	if err != nil {
		return nil, adminError(err)
	}
	return api.GetServer200JSONResponse(s.withRuntime(ctx, sv)), nil
}

func (s *apiServer) UpdateServer(ctx context.Context, req api.UpdateServerRequestObject) (api.UpdateServerResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	in := serverInput(req.Body)
	in.Version = ifMatch(req.Params.IfMatch)
	sv, err := s.servers.Update(ctx, adminRequest(ctx), req.Id.String(), in)
	if err != nil {
		return nil, adminError(err)
	}
	return api.UpdateServer200JSONResponse(s.withRuntime(ctx, sv)), nil
}

func (s *apiServer) DeleteServer(ctx context.Context, req api.DeleteServerRequestObject) (api.DeleteServerResponseObject, error) {
	if err := s.servers.Delete(ctx, adminRequest(ctx), req.Id.String()); err != nil {
		return nil, adminError(err)
	}
	return api.DeleteServer204Response{}, nil
}

func toAPIServerGroup(g servers.Group) api.ServerGroup {
	id, _ := uuid.Parse(g.ID)
	out := api.ServerGroup{Id: id, Name: g.Name, ServerIds: uuids(g.ServerIDs)}
	return out
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
		out.Items = append(out.Items, s.apiServerGroup(ctx, g))
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
	return api.CreateServerGroup201JSONResponse(s.apiServerGroup(ctx, g)), nil
}

func (s *apiServer) GetServerGroup(ctx context.Context, req api.GetServerGroupRequestObject) (api.GetServerGroupResponseObject, error) {
	g, err := s.servers.Group(ctx, adminRequest(ctx), req.Id.String())
	if err != nil {
		return nil, adminError(err)
	}
	return api.GetServerGroup200JSONResponse(s.apiServerGroup(ctx, g)), nil
}

func (s *apiServer) UpdateServerGroup(ctx context.Context, req api.UpdateServerGroupRequestObject) (api.UpdateServerGroupResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	g, err := s.servers.UpdateGroup(ctx, adminRequest(ctx), req.Id.String(), req.Body.Name, idStrings(req.Body.ServerIds), ifMatch(req.Params.IfMatch))
	if err != nil {
		return nil, adminError(err)
	}
	return api.UpdateServerGroup200JSONResponse(s.apiServerGroup(ctx, g)), nil
}

func (s *apiServer) DeleteServerGroup(ctx context.Context, req api.DeleteServerGroupRequestObject) (api.DeleteServerGroupResponseObject, error) {
	if err := s.servers.DeleteGroup(ctx, adminRequest(ctx), req.Id.String()); err != nil {
		return nil, adminError(err)
	}
	return api.DeleteServerGroup204Response{}, nil
}

func (s *apiServer) ServerAction(ctx context.Context, req api.ServerActionRequestObject) (api.ServerActionResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	r, err := s.servers.Action(ctx, adminRequest(ctx), req.Id.String(), string(req.Body.Action))
	if err != nil {
		return nil, actionError(err)
	}
	return api.ServerAction200JSONResponse(api.ServerActionResult{
		Action: api.ServerActionResultAction(r.Action), Via: api.ServerActionResultVia(r.Via),
	}), nil
}

func (s *apiServer) GetServerLogs(ctx context.Context, req api.GetServerLogsRequestObject) (api.GetServerLogsResponseObject, error) {
	n := servers.DefaultTailLines
	if req.Params.Lines != nil {
		n = *req.Params.Lines
	}
	if n < 1 || n > servers.MaxTailLines {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "lines must be 1 to 500"}
	}
	t, err := s.servers.Logs(ctx, adminRequest(ctx), req.Id.String(), n)
	if err != nil {
		return nil, actionError(err)
	}
	id, _ := uuid.Parse(t.ServerID)
	lines := t.Lines
	if lines == nil {
		lines = []string{}
	}
	return api.GetServerLogs200JSONResponse(api.LogTail{ServerId: id, Container: t.Container, Lines: lines, Truncated: t.Truncated}), nil
}

// actionError maps the errors of an action to Design 3.5 (revision 2.3
// candidate 6).
func actionError(err error) error {
	var failed *servers.ActionFailedError
	switch {
	case errors.Is(err, servers.ErrAgentUnavailable):
		return &problem{status: http.StatusServiceUnavailable, reason: api.ReasonCodeAgentUnavailable, detail: err.Error()}
	case errors.Is(err, servers.ErrAgentUnsupported):
		return &problem{status: http.StatusConflict, reason: api.ReasonCodeAgentUnsupported, detail: err.Error()}
	case errors.Is(err, servers.ErrRCONUnavailable):
		return &problem{status: http.StatusServiceUnavailable, reason: api.ReasonCodeServerUnavailable}
	case errors.Is(err, servers.ErrActionTimeout):
		return &problem{status: http.StatusGatewayTimeout, reason: api.ReasonCodeTimeout, detail: err.Error()}
	case errors.Is(err, servers.ErrActionRateLimited):
		return &problem{status: http.StatusTooManyRequests, reason: api.ReasonCodeRateLimited, retryAfter: 6}
	case errors.As(err, &failed):
		return &problem{status: http.StatusBadGateway, reason: api.ReasonCodeActionFailed, detail: failed.Error()}
	}
	return adminError(err)
}

// RuntimeSource provides the runtime data of a server (agentview).
type RuntimeSource interface {
	Runtime(ctx context.Context, serverID string, agentData bool) agentview.Runtime
	Containers(agentID string) ([]agentview.Container, bool)
}

// withRuntime adds the runtime data; the container part only for those
// who see the server's details, the admins in scope (UI-006).
func (s *apiServer) withRuntime(ctx context.Context, sv servers.Server) api.Server {
	out := toAPIServer(sv)
	out.Version = s.serverVersions(ctx)[sv.ID]
	if s.runtime == nil {
		return out
	}
	rt := s.runtime.Runtime(ctx, sv.ID, sv.Detailed)
	id, _ := uuid.Parse(rt.ServerID)
	ar := &api.ServerRuntime{Kind: api.Runtime, ServerId: id, Players: rt.Players, PlayersAt: rt.PlayersAt}
	if rt.PlayersStale {
		ar.PlayersStale = &rt.PlayersStale
	}
	if rt.GameVersion != "" {
		ar.GameVersion = &rt.GameVersion
	}
	if c := rt.Container; c != nil {
		ac := &api.ContainerRuntime{
			Name: c.Name, State: c.State, StartedAt: c.StartedAt, RestartCount: c.RestartCount, RestartLoop: c.RestartLoop,
			CpuPercent: c.CPUPercent, MetricsAt: c.MetricsAt, Stale: c.Stale, AgentConnected: c.AgentConnected,
			Capabilities: c.Capabilities,
		}
		if c.Image != "" {
			ac.Image = &c.Image
		}
		if len(c.Volumes) > 0 {
			vols := make([]api.VolumeRuntime, 0, len(c.Volumes))
			for _, vv := range c.Volumes {
				vr := api.VolumeRuntime{Name: vv.Name, SizeBytes: vv.SizeBytes, MeasuredAt: vv.MeasuredAt}
				if vv.Stale {
					stale := true
					vr.Stale = &stale
				}
				vols = append(vols, vr)
			}
			ac.Volumes = &vols
		}
		if c.MemBytes != nil {
			v := int64(min(*c.MemBytes, 1<<62)) //nolint:gosec // bounded
			ac.MemBytes = &v
		}
		if c.MemLimit != nil {
			v := int64(min(*c.MemLimit, 1<<62)) //nolint:gosec // bounded
			ac.MemLimit = &v
		}
		ar.Container = ac
	}
	out.Runtime = ar
	return out
}

// apiServerGroup adds the stored version (revision 2.4 candidate 24).
func (s *apiServer) apiServerGroup(ctx context.Context, g servers.Group) api.ServerGroup {
	out := toAPIServerGroup(g)
	out.Version = s.serverGroupVersions(ctx)[g.ID]
	return out
}
