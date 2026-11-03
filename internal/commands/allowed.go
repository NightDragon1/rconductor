// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/users"
)

// AllowedCommands is what a caller may send on a server: the command buttons of
// moderators and supporters (CMD-003) and the completion of the console
// (CMD-015).
type AllowedCommands struct {
	// FreeInput is set for RCON admin and server admin (CMD-002).
	FreeInput bool
	// Commands are the allowed paths with the catalog entry of the
	// profile where one exists; for admins the whole catalog.
	Commands []profiles.Command
	// PlayerPattern is the profile's syntax.player_pattern, so that the
	// command page checks player fields as the pipeline does.
	PlayerPattern string
}

// AllowedCommands lists the commands the caller may send on a server (revision 2.4
// candidate 5). Outside the scope the server does not exist (404).
func (s *Service) AllowedCommands(ctx context.Context, req users.Request, serverID string) (AllowedCommands, error) {
	rights, err := s.perm.For(ctx, req.Actor.ID)
	if err != nil {
		return AllowedCommands{}, err
	}
	sr, ok := rights.Server(serverID)
	if !ok {
		perm.LogDenied(s.log, perm.Denial{
			RequestID: req.RequestID, SessionRef: req.SessionRef, User: req.Pseudonym, Role: req.Actor.Role,
			Server: serverID, Reason: "not_found", Action: "commands.list",
		})
		return AllowedCommands{}, users.ErrNotFound
	}
	catalog, err := s.catalog(ctx, serverID)
	if err != nil {
		return AllowedCommands{}, err
	}
	out := AllowedCommands{FreeInput: sr.AllCommands, Commands: []profiles.Command{}}
	if sr.AllCommands {
		out.Commands = append(out.Commands, catalog...)
		return out, nil
	}
	syn, err := s.syntax(ctx, serverID)
	if err != nil {
		return AllowedCommands{}, err
	}
	out.PlayerPattern = syn.PlayerPattern
	byPath := map[string]profiles.Command{}
	for _, c := range catalog {
		byPath[c.Path] = c
	}
	for _, path := range sr.Commands {
		// Wrappers are refused for these roles anyway (CMD-005).
		if IsWrapper(strings.Fields(path), syn) {
			continue
		}
		c, ok := byPath[path]
		if !ok {
			// A command without catalog entry gets a free field for the
			// rest of the line (Design 7.3).
			c = profiles.Command{Path: path, Params: []profiles.Param{}}
		}
		out.Commands = append(out.Commands, c)
	}
	return out, nil
}

func (s *Service) catalog(ctx context.Context, serverID string) ([]profiles.Command, error) {
	r, err := s.st.Queries.GetServer(ctx, serverID)
	if err != nil || !r.ProfileID.Valid {
		return nil, err
	}
	rows, err := s.st.Queries.ListCatalogOfProfile(ctx, r.ProfileID.String)
	if err != nil {
		return nil, err
	}
	out := make([]profiles.Command, 0, len(rows))
	for _, row := range rows {
		c := profiles.Command{Path: row.Path, Description: row.Description, Params: []profiles.Param{}}
		_ = json.Unmarshal([]byte(row.Params), &c.Params)
		if c.Params == nil {
			c.Params = []profiles.Param{}
		}
		out = append(out, c)
	}
	return out, nil
}
