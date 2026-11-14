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
	// Ports by role (D-056): the RCON port only for the RCON admin, game
	// ports also for server admins, none for moderators and supporters.
	gamePorts := admin || perm.Role(p.Role) == perm.ServerAdmin
	out := api.ProfileList{Items: make([]api.GameProfile, 0, len(list))}
	for _, sp := range list {
		id, _ := uuid.Parse(sp.ID)
		r := sp.Rules
		g := api.GameProfile{
			Id: id, Slug: sp.Slug, Name: sp.Name, Kind: api.GameProfileKind(profiles.KindOf(&sp.Rules)), Protocol: sp.Protocol,
			Overridden: sp.Overridden, ImportedAt: sp.ImportedAt,
			CaseSensitive: *r.Syntax.CaseSensitive, StripPrefix: *r.Syntax.StripPrefix, MaxLength: r.Syntax.MaxLength,
			Wrappers: r.Syntax.Wrappers, Catalog: []api.CatalogCommand{},
		}
		if admin {
			g.DefaultPort = &sp.DefaultPort
		}
		if gamePorts {
			ports := make([]api.GamePort, 0, len(r.Ports.Game))
			for _, gp := range r.Ports.Game {
				ports = append(ports, api.GamePort{Port: gp.Port, Proto: api.GamePortProto(gp.Proto), Name: gp.Name})
			}
			g.GamePorts = &ports
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

// GetServerProfile is the effective profile of a server for the profile
// browser (UI-020): the merge the backend uses for the server (SRV-008,
// D-074), read only. The server must be in the caller's scope (otherwise
// 404); game ports only for the RCON admin and the server admin (D-056).
func (s *apiServer) GetServerProfile(ctx context.Context, req api.GetServerProfileRequestObject) (api.GetServerProfileResponseObject, error) {
	id := req.Id.String()
	if _, err := s.servers.Get(ctx, adminRequest(ctx), id); err != nil {
		return nil, adminError(err)
	}
	eff, ids, err := profiles.ForServer(ctx, s.store.Queries, id)
	if err != nil {
		return nil, err
	}
	catalog, from, err := profiles.CatalogWithSources(ctx, s.store.Queries, id)
	if err != nil {
		return nil, err
	}
	out := api.EffectiveProfile{
		ProfileIds: make([]uuid.UUID, 0, len(ids)), Wrappers: []string{}, Separators: []string{},
		Catalog: make([]api.EffectiveCommand, 0, len(catalog)),
	}
	for _, pid := range ids {
		u, _ := uuid.Parse(pid)
		out.ProfileIds = append(out.ProfileIds, u)
	}
	if eff != nil {
		r := eff.Syntax
		out.Protocol, out.MaxLength = eff.Protocol.Type, r.MaxLength
		if r.CaseSensitive != nil {
			out.CaseSensitive = *r.CaseSensitive
		}
		if r.StripPrefix != nil {
			out.StripPrefix = *r.StripPrefix
		}
		out.Wrappers = append(out.Wrappers, r.Wrappers...)
		out.Separators = append(out.Separators, r.Separators...)
		p, _ := principal(ctx)
		if role := perm.Role(p.Role); role == perm.RCONAdmin || role == perm.ServerAdmin {
			ports := make([]api.GamePort, 0, len(eff.Ports.Game))
			for _, gp := range eff.Ports.Game {
				ports = append(ports, api.GamePort{Port: gp.Port, Proto: api.GamePortProto(gp.Proto), Name: gp.Name})
			}
			out.GamePorts = &ports
		}
	}
	for i, c := range catalog {
		pc := toAPICatalog(c)
		u, _ := uuid.Parse(from[i])
		out.Catalog = append(out.Catalog, api.EffectiveCommand{Path: pc.Path, Description: pc.Description, Params: pc.Params, ProfileId: u})
	}
	return api.GetServerProfile200JSONResponse(out), nil
}
