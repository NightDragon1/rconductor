// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/agents"
	"github.com/NightDragon1/rconductor/internal/api"
)

func toAPIAgent(a agents.Agent) api.Agent {
	id, _ := uuid.Parse(a.ID)
	out := api.Agent{
		Id: id, Name: a.Name, Fingerprint: a.Fingerprint, Status: api.AgentStatus(a.Status), Version: a.Version,
		Capabilities: a.Capabilities, Connected: a.Connected, CreatedAt: a.CreatedAt,
		PairedBy: uuidPtr(a.PairedBy), LastSeenAt: a.LastSeenAt, RevokedAt: a.RevokedAt,
	}
	if a.Remote != "" {
		out.Remote = &a.Remote
	}
	return out
}

func (s *apiServer) ListAgents(ctx context.Context, _ api.ListAgentsRequestObject) (api.ListAgentsResponseObject, error) {
	list, err := s.agents.List(ctx, adminRequest(ctx))
	if err != nil {
		return nil, adminError(err)
	}
	out := api.AgentList{Items: make([]api.Agent, 0, len(list))}
	for _, a := range list {
		out.Items = append(out.Items, toAPIAgent(a))
	}
	return api.ListAgents200JSONResponse(out), nil
}

func (s *apiServer) GetAgent(ctx context.Context, req api.GetAgentRequestObject) (api.GetAgentResponseObject, error) {
	a, err := s.agents.Get(ctx, adminRequest(ctx), req.Id.String())
	if err != nil {
		return nil, adminError(err)
	}
	return api.GetAgent200JSONResponse(toAPIAgent(a)), nil
}

func (s *apiServer) ListAgentContainers(ctx context.Context, req api.ListAgentContainersRequestObject) (api.ListAgentContainersResponseObject, error) {
	// The agent's own check decides: RCON admin only, 404 for an unknown
	// agent (revision 2.4 candidate 22).
	if _, err := s.agents.Get(ctx, adminRequest(ctx), req.Id.String()); err != nil {
		return nil, adminError(err)
	}
	out := api.AgentContainers{Items: []struct {
		Image *string `json:"image,omitempty"`
		Name  string  `json:"name"`
		State string  `json:"state"`
	}{}}
	if s.runtime == nil {
		return api.ListAgentContainers200JSONResponse(out), nil
	}
	list, connected := s.runtime.Containers(req.Id.String())
	out.Connected = connected
	for _, c := range list {
		item := struct {
			Image *string `json:"image,omitempty"`
			Name  string  `json:"name"`
			State string  `json:"state"`
		}{Name: c.Name, State: c.State}
		if c.Image != "" {
			img := c.Image
			item.Image = &img
		}
		out.Items = append(out.Items, item)
	}
	return api.ListAgentContainers200JSONResponse(out), nil
}

func (s *apiServer) DeleteAgent(ctx context.Context, req api.DeleteAgentRequestObject) (api.DeleteAgentResponseObject, error) {
	if err := s.agents.Delete(ctx, adminRequest(ctx), req.Id.String()); err != nil {
		return nil, adminError(err)
	}
	return api.DeleteAgent204Response{}, nil
}

func (s *apiServer) ApproveAgent(ctx context.Context, req api.ApproveAgentRequestObject) (api.ApproveAgentResponseObject, error) {
	a, err := s.agents.Approve(ctx, adminRequest(ctx), req.Id.String())
	if err != nil {
		return nil, adminError(err)
	}
	return api.ApproveAgent200JSONResponse(toAPIAgent(a)), nil
}

func (s *apiServer) CreateAgentToken(ctx context.Context, _ api.CreateAgentTokenRequestObject) (api.CreateAgentTokenResponseObject, error) {
	t, err := s.agents.CreateToken(ctx, adminRequest(ctx))
	if err != nil {
		return nil, adminError(err)
	}
	out := api.AgentToken{Token: t.Token, ExpiresAt: t.ExpiresAt, PanelFingerprint: t.PanelFingerprint, Compose: t.Compose}
	if t.PanelAddr != "" {
		out.PanelAddr = &t.PanelAddr
	}
	return api.CreateAgentToken201JSONResponse(out), nil
}
