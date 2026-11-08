// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/commands"
)

// Command templates (CMD-008, D-068): the interface fills them and sends
// the command through SendCommand; here they are only stored.

func toAPITemplate(t commands.Template) api.Template {
	id, _ := uuid.Parse(t.ID)
	server, _ := uuid.Parse(t.ServerID)
	return api.Template{
		Id: id, ServerId: server, Name: t.Name, Template: t.Template,
		Placeholders: t.Placeholders, Version: versionString(t.Version),
	}
}

func (s *apiServer) ListTemplates(ctx context.Context, req api.ListTemplatesRequestObject) (api.ListTemplatesResponseObject, error) {
	ts, err := s.commands.Templates(ctx, adminRequest(ctx), req.Id.String())
	if err != nil {
		return nil, adminError(err)
	}
	out := api.TemplateList{Items: make([]api.Template, 0, len(ts))}
	for _, t := range ts {
		out.Items = append(out.Items, toAPITemplate(t))
	}
	return api.ListTemplates200JSONResponse(out), nil
}

func (s *apiServer) GetTemplate(ctx context.Context, req api.GetTemplateRequestObject) (api.GetTemplateResponseObject, error) {
	t, err := s.commands.GetTemplate(ctx, adminRequest(ctx), req.Id.String(), req.TemplateId.String())
	if err != nil {
		return nil, adminError(err)
	}
	return api.GetTemplate200JSONResponse(toAPITemplate(t)), nil
}

func (s *apiServer) CreateTemplate(ctx context.Context, req api.CreateTemplateRequestObject) (api.CreateTemplateResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	t, err := s.commands.CreateTemplate(ctx, adminRequest(ctx), req.Id.String(), req.Body.Name, req.Body.Template)
	if err != nil {
		return nil, adminError(err)
	}
	return api.CreateTemplate201JSONResponse(toAPITemplate(t)), nil
}

func (s *apiServer) UpdateTemplate(ctx context.Context, req api.UpdateTemplateRequestObject) (api.UpdateTemplateResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	t, err := s.commands.UpdateTemplate(ctx, adminRequest(ctx), req.Id.String(), req.TemplateId.String(),
		req.Body.Name, req.Body.Template, ifMatch(req.Params.IfMatch))
	if err != nil {
		return nil, adminError(err)
	}
	return api.UpdateTemplate200JSONResponse(toAPITemplate(t)), nil
}

func (s *apiServer) DeleteTemplate(ctx context.Context, req api.DeleteTemplateRequestObject) (api.DeleteTemplateResponseObject, error) {
	if err := s.commands.DeleteTemplate(ctx, adminRequest(ctx), req.Id.String(), req.TemplateId.String()); err != nil {
		return nil, adminError(err)
	}
	return api.DeleteTemplate204Response{}, nil
}
