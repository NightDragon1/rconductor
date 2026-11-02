// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"fmt"
	"slices"

	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// MaxFavorites bounds the favorite servers of a user.
const MaxFavorites = 50

// Favorites returns the caller's favorite servers in his order; servers
// that left his scope are left out (CMD-001, revision 2.4 candidate 6).
func (s *Service) Favorites(ctx context.Context, req Request) ([]string, error) {
	ids, err := s.st.Queries.ListFavorites(ctx, req.Actor.ID)
	if err != nil {
		return nil, err
	}
	rights, err := s.perm.For(ctx, req.Actor.ID)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, id := range ids {
		if _, ok := rights.Server(id); ok {
			out = append(out, id)
		}
	}
	return out, nil
}

// SetFavorites replaces the caller's favorites; only servers in his scope,
// at most MaxFavorites, duplicates once.
func (s *Service) SetFavorites(ctx context.Context, req Request, ids []string) ([]string, error) {
	rights, err := s.perm.For(ctx, req.Actor.ID)
	if err != nil {
		return nil, err
	}
	var clean []string
	for _, id := range ids {
		if slices.Contains(clean, id) {
			continue
		}
		if _, ok := rights.Server(id); !ok {
			return nil, &InvalidError{Detail: "unknown server " + id}
		}
		clean = append(clean, id)
	}
	if len(clean) > MaxFavorites {
		return nil, &InvalidError{Detail: fmt.Sprintf("at most %d favorites", MaxFavorites)}
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	if err := q.DeleteFavorites(ctx, req.Actor.ID); err != nil {
		return nil, err
	}
	for i, id := range clean {
		if err := q.InsertFavorite(ctx, sqlitedb.InsertFavoriteParams{UserID: req.Actor.ID, ServerID: id, Position: int64(i)}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Favorites(ctx, req)
}
