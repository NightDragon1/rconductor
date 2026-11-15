// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/users"
)

// GetServerGroupSayPrefix reads the prefix of messages to the players for
// a group (CMD-020, D-082): the RCON admin and a server admin with every
// server of the group in scope; the version is that of the group.
func (s *apiServer) GetServerGroupSayPrefix(ctx context.Context, req api.GetServerGroupSayPrefixRequestObject) (api.GetServerGroupSayPrefixResponseObject, error) {
	id := req.Id.String()
	p, err := s.servers.SayPrefix(ctx, adminRequest(ctx), id)
	if err != nil {
		return nil, adminError(err)
	}
	return api.GetServerGroupSayPrefix200JSONResponse{SayPrefix: p, Version: s.serverGroupVersions(ctx)[id]}, nil
}

// SetServerGroupSayPrefix sets or clears the prefix of a group (CMD-020,
// D-082), with If-Match on the version of the group.
func (s *apiServer) SetServerGroupSayPrefix(ctx context.Context, req api.SetServerGroupSayPrefixRequestObject) (api.SetServerGroupSayPrefixResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	id := req.Id.String()
	p, err := s.servers.SetSayPrefix(ctx, adminRequest(ctx), id, req.Body.SayPrefix, ifMatch(req.Params.IfMatch))
	// An invalid prefix is 400 (D-082), not 422 as other invalid input.
	if inv := (*users.InvalidError)(nil); errors.As(err, &inv) {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: inv.Detail}
	}
	if err != nil {
		return nil, adminError(err)
	}
	return api.SetServerGroupSayPrefix200JSONResponse{SayPrefix: p, Version: s.serverGroupVersions(ctx)[id]}, nil
}
