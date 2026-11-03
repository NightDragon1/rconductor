// SPDX-License-Identifier: AGPL-3.0-or-later

package perm

import (
	"context"
	"fmt"

	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

type sqlitedbUser = sqlitedb.ListManageCandidatesRow

// Actor is the user who performs an administration request.
type Actor struct {
	ID   string
	Role Role
}

// Managed returns the IDs of the users actor may manage (Design 3.6,
// ROLE-008): only users with a lower role, in the actor's scope.
//
//   - RCON admin: every other user with a lower role.
//   - Server admin: moderators he invited or who are members of one of his
//     moderator groups, the supporters of those moderators, and supporters
//     he invited.
//   - Moderator: supporters he invited or assigned to a server, only while
//     supporters are enabled globally and for him (ROLE-006).
//   - Supporter: nobody.
//
// Soft-deleted users are never managed; disabled users are.
func (c *Checker) Managed(ctx context.Context, actor Actor) (map[string]bool, error) {
	q := c.st.Queries
	users, err := q.ListManageCandidates(ctx)
	if err != nil {
		return nil, fmt.Errorf("load users: %w", err)
	}
	out := map[string]bool{}
	switch actor.Role {
	case RCONAdmin:
		for _, u := range users {
			if actor.Role.Above(Role(u.Role)) {
				out[u.ID] = true
			}
		}
		return out, nil
	case ServerAdmin, Moderator:
	default:
		return out, nil
	}

	links, err := q.ListSupporterLinks(ctx)
	if err != nil {
		return nil, fmt.Errorf("load supporter links: %w", err)
	}
	supportersOf := func(moderators map[string]bool) {
		for _, u := range users {
			if Role(u.Role) == Supporter && u.CreatedBy.Valid && moderators[u.CreatedBy.String] {
				out[u.ID] = true
			}
		}
		for _, l := range links {
			if moderators[l.ModeratorID] {
				out[l.SupporterID] = true
			}
		}
	}

	if actor.Role == Moderator {
		var enabled bool
		if err := c.st.GlobalSetting(ctx, SettingSupportersEnabled, &enabled); err != nil || !enabled {
			return out, nil //nolint:nilerr // a missing switch means off
		}
		for _, u := range users {
			if u.ID == actor.ID && u.SupportersAllowed == 1 {
				supportersOf(map[string]bool{actor.ID: true})
			}
		}
		c.dropNonRole(out, users, Supporter)
		return out, nil
	}

	owners, err := q.ListModeratorGroupOwners(ctx)
	if err != nil {
		return nil, fmt.Errorf("load moderator groups: %w", err)
	}
	moderators := map[string]bool{}
	for _, o := range owners {
		if o.OwnerID == actor.ID {
			moderators[o.UserID] = true
		}
	}
	for _, u := range users {
		if u.CreatedBy.Valid && u.CreatedBy.String == actor.ID {
			out[u.ID] = true // invited moderators and supporters
			if Role(u.Role) == Moderator {
				moderators[u.ID] = true
			}
		}
	}
	c.dropNonRole(moderators, users, Moderator)
	for id := range moderators {
		out[id] = true
	}
	supportersOf(moderators)
	c.dropNonRole(out, users, Moderator, Supporter)
	return out, nil
}

// dropNonRole removes IDs whose user does not have one of roles.
func (c *Checker) dropNonRole(ids map[string]bool, users []sqlitedbUser, roles ...Role) {
	role := map[string]Role{}
	for _, u := range users {
		role[u.ID] = Role(u.Role)
	}
	for id := range ids {
		keep := false
		for _, r := range roles {
			keep = keep || role[id] == r
		}
		if !keep {
			delete(ids, id)
		}
	}
}

// MayManageUsers reports whether actor may use the user administration at
// all (Design 2.2, 3.6): the RCON admin and server admins; a moderator only
// while supporters are released globally and for him; a supporter never.
func (c *Checker) MayManageUsers(ctx context.Context, actor Actor) (bool, error) {
	switch actor.Role {
	case RCONAdmin, ServerAdmin:
		return true, nil
	case Moderator:
	default:
		return false, nil
	}
	var enabled bool
	if err := c.st.GlobalSetting(ctx, SettingSupportersEnabled, &enabled); err != nil || !enabled {
		return false, nil //nolint:nilerr // a missing switch means off
	}
	u, err := c.st.Queries.GetRightsUser(ctx, actor.ID)
	if err != nil {
		return false, fmt.Errorf("load user: %w", err)
	}
	return u.SupportersAllowed == 1, nil
}

// Manages reports whether actor may manage the user target.
func (c *Checker) Manages(ctx context.Context, actor Actor, target string) (bool, error) {
	m, err := c.Managed(ctx, actor)
	if err != nil {
		return false, err
	}
	return m[target], nil
}
