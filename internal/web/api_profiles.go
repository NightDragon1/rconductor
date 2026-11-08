// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/profiles"
)

func (s *apiServer) ListProfiles(ctx context.Context, _ api.ListProfilesRequestObject) (api.ListProfilesResponseObject, error) {
	list, err := profiles.List(ctx, s.store)
	if err != nil {
		return nil, err
	}
	p, _ := principal(ctx)
	admin := perm.Role(p.Role) == perm.RCONAdmin
	out := api.ProfileList{Items: make([]api.GameProfile, 0, len(list))}
	for _, sp := range list {
		id, _ := uuid.Parse(sp.ID)
		r := sp.Rules
		g := api.GameProfile{
			Id: id, Slug: sp.Slug, Name: sp.Name, Kind: api.GameProfileKind(profiles.KindOf(&sp.Rules)), Protocol: sp.Protocol, DefaultPort: sp.DefaultPort,
			Overridden: sp.Overridden, ImportedAt: sp.ImportedAt,
			CaseSensitive: *r.Syntax.CaseSensitive, StripPrefix: *r.Syntax.StripPrefix, MaxLength: r.Syntax.MaxLength,
			Wrappers: r.Syntax.Wrappers, GamePorts: []api.GamePort{}, Catalog: []api.CatalogCommand{},
		}
		for _, gp := range r.Ports.Game {
			g.GamePorts = append(g.GamePorts, api.GamePort{Port: gp.Port, Proto: api.GamePortProto(gp.Proto), Name: gp.Name})
		}
		for _, c := range r.Catalog {
			g.Catalog = append(g.Catalog, toAPICatalog(c))
		}
		if admin && sp.LastError != "" {
			g.LastError = &sp.LastError
			g.LastErrorAt = sp.LastErrorAt
		}
		out.Items = append(out.Items, g)
	}
	return api.ListProfiles200JSONResponse(out), nil
}

func toAPICatalog(c profiles.Command) api.CatalogCommand {
	cmd := api.CatalogCommand{Path: c.Path, Description: c.Description, Params: []api.ProfileParam{}}
	for _, pa := range c.Params {
		param := api.ProfileParam{Name: pa.Name, Type: api.ProfileParamType(pa.Type), Min: pa.Min, Max: pa.Max}
		if pa.Optional {
			param.Optional = &pa.Optional
		}
		if len(pa.Values) > 0 {
			v := pa.Values
			param.Values = &v
		}
		cmd.Params = append(cmd.Params, param)
	}
	return cmd
}
