// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/cache"
)

func (s *apiServer) GetPlayers(ctx context.Context, req api.GetPlayersRequestObject) (api.GetPlayersResponseObject, error) {
	// The server must be in the caller's scope; otherwise 404.
	if _, err := s.servers.Get(ctx, adminRequest(ctx), req.Id.String()); err != nil {
		return nil, adminError(err)
	}
	p, err := s.cache.Players(ctx, req.Id.String())
	switch {
	case errors.Is(err, cache.ErrNoPlayerList):
		return api.GetPlayers200JSONResponse(api.PlayerList{Available: false, Players: []string{}}), nil
	case err != nil:
		return nil, &problem{status: http.StatusServiceUnavailable, reason: api.ReasonCodeServerUnavailable}
	}
	if p.Unknown {
		return api.GetPlayers200JSONResponse(api.PlayerList{Available: false, Players: []string{}}), nil
	}
	// count always; max only if the game tells it (PROT-003).
	count := p.Count
	out := api.PlayerList{Available: true, Players: p.Names, Stale: p.Stale, Count: &count}
	if p.Max > 0 {
		limit := p.Max
		out.Max = &limit
	}
	if out.Players == nil {
		out.Players = []string{}
	}
	if !p.UpdatedAt.IsZero() {
		out.UpdatedAt = &p.UpdatedAt
	}
	return api.GetPlayers200JSONResponse(out), nil
}
