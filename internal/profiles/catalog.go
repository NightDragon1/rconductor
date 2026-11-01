// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/NightDragon1/rconductor/internal/store"
)

// Stored is a profile as imported into the database.
type Stored struct {
	ID          string
	Slug        string
	Name        string
	Protocol    string
	DefaultPort int
	Overridden  bool
	ImportedAt  time.Time
	LastError   string
	LastErrorAt *time.Time
	Rules       Profile // syntax, ports, players, stop; catalog filled from catalog_commands
}

// List returns all imported profiles with their catalog; the UI works
// only from these (CAT-003).
func List(ctx context.Context, st *store.Store) ([]Stored, error) {
	rows, err := st.Queries.ListGameProfiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	cmds, err := st.Queries.ListCatalog(ctx)
	if err != nil {
		return nil, fmt.Errorf("list catalog: %w", err)
	}
	byProfile := map[string][]Command{}
	for _, c := range cmds {
		cmd := Command{Path: c.Path, Description: c.Description, Params: []Param{}}
		_ = json.Unmarshal([]byte(c.Params), &cmd.Params)
		byProfile[c.ProfileID] = append(byProfile[c.ProfileID], cmd)
	}
	out := make([]Stored, 0, len(rows))
	for _, r := range rows {
		s := Stored{
			ID: r.ID, Slug: r.Slug, Name: r.Name, Protocol: r.Protocol, DefaultPort: int(r.DefaultPort),
			Overridden: r.Overridden == 1, LastError: r.LastError.String,
		}
		s.ImportedAt, _ = time.Parse(store.TimeLayout, r.ImportedAt)
		if r.LastErrorAt.Valid {
			if t, err := time.Parse(store.TimeLayout, r.LastErrorAt.String); err == nil {
				s.LastErrorAt = &t
			}
		}
		if err := json.Unmarshal([]byte(r.Rules), &s.Rules); err != nil {
			return nil, fmt.Errorf("rules of profile %s: %w", r.Slug, err)
		}
		s.Rules.Catalog = byProfile[r.ID]
		if s.Rules.Catalog == nil {
			s.Rules.Catalog = []Command{}
		}
		out = append(out, s)
	}
	return out, nil
}
