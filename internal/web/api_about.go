// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/agents"
	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/buildinfo"
	"github.com/NightDragon1/rconductor/internal/foreign"
	"github.com/NightDragon1/rconductor/internal/perm"
)

// maxAgentVersion is the longest agent version shown (D-080).
const maxAgentVersion = 64

// GetAbout tells name, version, license and repository to everyone; with a
// session also commit and build date, and to the RCON admin the paired
// agents with their version (UI-024, D-080). A session that must change its
// password or set up the second factor first gets the anonymous answer.
func (s *apiServer) GetAbout(ctx context.Context, _ api.GetAboutRequestObject) (api.GetAboutResponseObject, error) {
	// The answer depends on the session: no cache may keep it.
	if h := httpOf(ctx); h.w != nil {
		h.w.Header().Set("Cache-Control", "no-store")
	}
	out := api.About{
		Name: buildinfo.Name, Version: buildinfo.Version, License: buildinfo.License, RepoUrl: buildinfo.RepoURL,
	}
	p, ok := principal(ctx)
	if !ok || p.MustChangePassword || p.TOTPSetupRequired {
		return api.GetAbout200JSONResponse(out), nil
	}
	commit, date := buildinfo.Commit, buildinfo.BuildDate
	out.Commit, out.BuildDate = &commit, &date
	if perm.Role(p.Role) != perm.RCONAdmin {
		return api.GetAbout200JSONResponse(out), nil
	}
	list, err := s.agents.List(ctx, adminRequest(ctx))
	if err != nil {
		return nil, adminError(err)
	}
	paired := make([]api.AboutAgent, 0, len(list))
	for _, a := range list {
		if a.Status != agents.StatusActive {
			continue // pending pairings and revoked agents are no agents of the panel
		}
		id, _ := uuid.Parse(a.ID)
		paired = append(paired, api.AboutAgent{Id: id, Name: a.Name, Version: agentVersion(a.Version), Connected: a.Connected})
	}
	out.Agents = &paired
	return api.GetAbout200JSONResponse(out), nil
}

// agentVersion is the version an agent reported, cleaned as foreign data
// (SEC-006); missing or longer than maxAgentVersion it is unknown (D-080).
// The length counts after cleaning and before any cut, so a space at the
// cut cannot make an overlong version look short.
func agentVersion(v string) string {
	v = foreign.Name(v, len(v))
	if v == "" || utf8.RuneCountInString(v) > maxAgentVersion {
		return buildinfo.Unknown
	}
	return v
}
