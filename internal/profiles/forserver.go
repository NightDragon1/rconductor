// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// Loaded is one stored profile of a server: its id, protocol and rules.
type Loaded struct {
	ID       string
	Protocol string
	Rules    *Profile
}

// Load reads stored profiles by id, in the given order; an unknown id is
// an error.
func Load(ctx context.Context, q *sqlitedb.Queries, ids []string) ([]Loaded, error) {
	out := make([]Loaded, 0, len(ids))
	for _, id := range ids {
		gp, err := q.GetGameProfile(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("profile %s: %w", id, err)
		}
		var rules Profile
		if err := json.Unmarshal([]byte(gp.Rules), &rules); err != nil {
			return nil, fmt.Errorf("rules of profile %s: %w", gp.Slug, err)
		}
		rules.ID, rules.Protocol.Type, rules.Protocol.DefaultPort = gp.Slug, gp.Protocol, int(gp.DefaultPort)
		out = append(out, Loaded{ID: gp.ID, Protocol: gp.Protocol, Rules: &rules})
	}
	return out, nil
}

// ForServer is the effective profile of a server, merged from its profile
// list top first (SRV-008, D-074), and the ids of that list; nil without
// profiles.
func ForServer(ctx context.Context, q *sqlitedb.Queries, serverID string) (*Profile, []string, error) {
	ids, err := q.GetServerProfiles(ctx, serverID)
	if err != nil {
		return nil, nil, err
	}
	loaded, err := Load(ctx, q, ids)
	if err != nil {
		return nil, ids, err
	}
	list := make([]*Profile, len(loaded))
	for i, l := range loaded {
		list[i] = l.Rules
	}
	return Effective(list), ids, nil
}

// CatalogForServer is the catalog of a server: the union of the catalogs
// of its profiles, top first; the upper entry wins for the same path.
func CatalogForServer(ctx context.Context, q *sqlitedb.Queries, serverID string) ([]Command, error) {
	out, _, err := CatalogWithSources(ctx, q, serverID)
	return out, err
}

// CatalogWithSources is CatalogForServer plus, for each entry, the id of
// the profile it comes from (the upper one on the same path, UI-020).
func CatalogWithSources(ctx context.Context, q *sqlitedb.Queries, serverID string) ([]Command, []string, error) {
	ids, err := q.GetServerProfiles(ctx, serverID)
	if err != nil {
		return nil, nil, err
	}
	cats := make([][]Command, 0, len(ids))
	source := map[string]string{}
	for _, id := range ids {
		rows, err := q.ListCatalogOfProfile(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		cat := make([]Command, 0, len(rows))
		for _, r := range rows {
			c := Command{Path: r.Path, Description: r.Description, Params: []Param{}}
			_ = json.Unmarshal([]byte(r.Params), &c.Params)
			cat = append(cat, c)
			if path := NormalizePath(c.Path); source[path] == "" {
				source[path] = id
			}
		}
		cats = append(cats, cat)
	}
	out := MergeCatalogs(cats)
	from := make([]string, len(out))
	for i, c := range out {
		from[i] = source[NormalizePath(c.Path)]
	}
	return out, from, nil
}
