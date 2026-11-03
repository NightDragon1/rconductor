// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/users"
)

func toAPIList(l users.CommandList) api.CommandList {
	id, _ := uuid.Parse(l.ID)
	owner, _ := uuid.Parse(l.OwnerID)
	out := api.CommandList{Id: id, Name: l.Name, OwnerId: owner, Entries: []api.CommandListEntry{}}
	for _, e := range l.Entries {
		out.Entries = append(out.Entries, api.CommandListEntry{Path: e.Path, Passable: e.Passable})
	}
	out.Version = versionOf(out)
	return out
}

func entriesOf(in *[]api.CommandListEntry) *[]users.Entry {
	if in == nil {
		return nil
	}
	out := make([]users.Entry, 0, len(*in))
	for _, e := range *in {
		out = append(out, users.Entry{Path: e.Path, Passable: e.Passable})
	}
	return &out
}

func (s *apiServer) ListCommandLists(ctx context.Context, _ api.ListCommandListsRequestObject) (api.ListCommandListsResponseObject, error) {
	ls, err := s.users.CommandLists(ctx, adminRequest(ctx))
	if err != nil {
		return nil, adminError(err)
	}
	out := api.CommandListList{Items: make([]api.CommandList, 0, len(ls))}
	for _, l := range ls {
		out.Items = append(out.Items, toAPIList(l))
	}
	return api.ListCommandLists200JSONResponse(out), nil
}

func (s *apiServer) CreateCommandList(ctx context.Context, req api.CreateCommandListRequestObject) (api.CreateCommandListResponseObject, error) {
	if req.Body == nil {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "body required"}
	}
	var entries []users.Entry
	if e := entriesOf(req.Body.Entries); e != nil {
		entries = *e
	}
	l, err := s.users.CreateCommandList(ctx, adminRequest(ctx), req.Body.Name, entries)
	if err != nil {
		return nil, adminError(err)
	}
	return api.CreateCommandList201JSONResponse(toAPIList(l)), nil
}

func (s *apiServer) GetCommandList(ctx context.Context, req api.GetCommandListRequestObject) (api.GetCommandListResponseObject, error) {
	l, err := s.users.CommandList(ctx, adminRequest(ctx), req.Id.String())
	if err != nil {
		return nil, adminError(err)
	}
	return api.GetCommandList200JSONResponse(toAPIList(l)), nil
}

func (s *apiServer) UpdateCommandList(ctx context.Context, req api.UpdateCommandListRequestObject) (api.UpdateCommandListResponseObject, error) {
	if req.Body == nil {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "body required"}
	}
	unlock := s.locks.lock("command-list:" + req.Id.String())
	defer unlock()
	if req.Params.IfMatch != nil {
		cur, err := s.users.CommandList(ctx, adminRequest(ctx), req.Id.String())
		if err != nil {
			return nil, adminError(err)
		}
		if !matches(req.Params.IfMatch, toAPIList(cur).Version) {
			return nil, errConflict()
		}
	}
	l, err := s.users.UpdateCommandList(ctx, adminRequest(ctx), req.Id.String(), req.Body.Name, entriesOf(req.Body.Entries))
	if err != nil {
		return nil, adminError(err)
	}
	return api.UpdateCommandList200JSONResponse(toAPIList(l)), nil
}

func (s *apiServer) DeleteCommandList(ctx context.Context, req api.DeleteCommandListRequestObject) (api.DeleteCommandListResponseObject, error) {
	if err := s.users.DeleteCommandList(ctx, adminRequest(ctx), req.Id.String()); err != nil {
		return nil, adminError(err)
	}
	return api.DeleteCommandList204Response{}, nil
}
