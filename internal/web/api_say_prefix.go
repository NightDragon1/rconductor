// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"

	"github.com/NightDragon1/rconductor/internal/api"
)

// GetServerGroupSayPrefix reads the prefix of messages to the players for
// a group (CMD-020, D-082); contract first, the group must be visible to
// the caller (otherwise 404) before anything else.
func (s *apiServer) GetServerGroupSayPrefix(ctx context.Context, req api.GetServerGroupSayPrefixRequestObject) (api.GetServerGroupSayPrefixResponseObject, error) {
	if _, err := s.servers.Group(ctx, adminRequest(ctx), req.Id.String()); err != nil {
		return nil, adminError(err)
	}
	return nil, notImplemented("getServerGroupSayPrefix")
}

// SetServerGroupSayPrefix sets the prefix of a group (CMD-020, D-082);
// contract first, like GetServerGroupSayPrefix.
func (s *apiServer) SetServerGroupSayPrefix(ctx context.Context, req api.SetServerGroupSayPrefixRequestObject) (api.SetServerGroupSayPrefixResponseObject, error) {
	if _, err := s.servers.Group(ctx, adminRequest(ctx), req.Id.String()); err != nil {
		return nil, adminError(err)
	}
	return nil, notImplemented("setServerGroupSayPrefix")
}
