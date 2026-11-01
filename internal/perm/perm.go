// SPDX-License-Identifier: AGPL-3.0-or-later

// Package perm is the only place that decides about permissions (Design
// 3.1, Architecture 4). Rights are computed from the database per user,
// cached in memory and dropped on every change; everything that is not
// explicitly allowed is denied.
package perm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/store"
)

// Role is one of the four fixed roles (ROLE-001).
type Role string

// The roles, from highest to lowest.
const (
	RCONAdmin   Role = "rcon_admin"
	ServerAdmin Role = "server_admin"
	Moderator   Role = "moderator"
	Supporter   Role = "supporter"
)

// Roles lists all roles from highest to lowest.
var Roles = []Role{RCONAdmin, ServerAdmin, Moderator, Supporter}

// Valid reports whether r is one of the four roles.
func (r Role) Valid() bool { return slices.Contains(Roles, r) }

// rank is 0 for the highest role and grows downwards; invalid roles rank
// below everything.
func (r Role) rank() int {
	if i := slices.Index(Roles, r); i >= 0 {
		return i
	}
	return len(Roles)
}

// Above reports whether r is strictly higher than o (ROLE-008).
func (r Role) Above(o Role) bool { return r.Valid() && r.rank() < o.rank() }

// SettingSupportersEnabled is the global supporter switch (ROLE-006); off
// by default, so supporters exist only after the RCON admin allows them.
const SettingSupportersEnabled = "supporters.enabled"

// EnsureDefaults stores the default permission settings unless they exist.
func EnsureDefaults(ctx context.Context, st *store.Store) error {
	_, err := st.EnsureDefault(ctx, SettingSupportersEnabled, false)
	return err
}

// ServerRights are the rights of one user on one server.
type ServerRights struct {
	// AllCommands allows any command (RCON admin and server admin).
	AllCommands bool
	// Commands are the allowed token paths otherwise, sorted.
	Commands []string
	// Passable are the paths a moderator may pass on to supporters.
	Passable []string
	// Agent allows start, stop, restart and logs via the agent (AGT-008).
	Agent bool
}

// Rights are the computed rights of one user.
type Rights struct {
	UserID  string
	Role    Role
	servers map[string]ServerRights
}

// Server returns the rights on a server; ok is false if the server is
// outside the scope, which callers answer with 404 (Design 3.1).
func (r *Rights) Server(id string) (ServerRights, bool) {
	s, ok := r.servers[id]
	return s, ok
}

// ServerIDs returns the servers in scope, sorted.
func (r *Rights) ServerIDs() []string {
	ids := make([]string, 0, len(r.servers))
	for id := range r.servers {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// Checker computes and caches rights.
type Checker struct {
	st  *store.Store
	log *slog.Logger

	mu    sync.Mutex
	gen   uint64
	cache map[string]*Rights
}

// NewChecker returns a checker on st.
func NewChecker(st *store.Store, log *slog.Logger) *Checker {
	return &Checker{st: st, log: log, cache: map[string]*Rights{}}
}

// Invalidate drops all cached rights. Every change to users, groups,
// memberships, scopes, grants, lists, servers or the supporter switch must
// call it after commit; scopes shrink for everyone below at once (Design
// 3.2).
func (c *Checker) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	clear(c.cache)
}

// For returns the rights of a user. Unknown, disabled and deleted users
// have no rights at all.
func (c *Checker) For(ctx context.Context, userID string) (*Rights, error) {
	c.mu.Lock()
	if r, ok := c.cache[userID]; ok {
		c.mu.Unlock()
		return r, nil
	}
	gen := c.gen
	c.mu.Unlock()

	r, err := c.compute(ctx, userID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.gen == gen {
		c.cache[userID] = r
	}
	c.mu.Unlock()
	c.log.Info("rights computed", logging.Event(logging.PermRecomputed),
		slog.String("user_id", userID), slog.String(logging.KeyRole, string(r.Role)),
		slog.Int("servers", len(r.servers)))
	return r, nil
}

func (c *Checker) compute(ctx context.Context, userID string) (*Rights, error) {
	q := c.st.Queries
	u, err := q.GetRightsUser(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return &Rights{UserID: userID, servers: map[string]ServerRights{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load user for rights: %w", err)
	}
	r := &Rights{UserID: userID, Role: Role(u.Role), servers: map[string]ServerRights{}}
	if u.DisabledAt.Valid || u.DeletedAt.Valid || !r.Role.Valid() {
		return r, nil
	}

	switch r.Role {
	case RCONAdmin, ServerAdmin:
		ids, err := c.adminServers(ctx, userID, r.Role)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			r.servers[id] = ServerRights{AllCommands: true, Agent: true}
		}
	case Moderator:
		lists, err := c.moderatorLists(ctx, userID)
		if err != nil {
			return nil, err
		}
		for id, l := range lists {
			r.servers[id] = ServerRights{Commands: l.paths(false), Passable: l.paths(true)}
		}
	case Supporter:
		if err := c.supporterRights(ctx, userID, r); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// adminServers returns all servers for the RCON admin and the scope of a
// server admin (ROLE-002, ROLE-003).
func (c *Checker) adminServers(ctx context.Context, userID string, role Role) ([]string, error) {
	var ids []string
	var err error
	if role == RCONAdmin {
		ids, err = c.st.Queries.ListActiveServerIDs(ctx)
	} else {
		ids, err = c.st.Queries.ListAdminScopeServerIDs(ctx, userID)
	}
	if err != nil {
		return nil, fmt.Errorf("load admin scope: %w", err)
	}
	return ids, nil
}

// list is a command list as union of entries: path → passable.
type list map[string]bool

func (l list) paths(passableOnly bool) []string {
	out := []string{}
	for p, passable := range l {
		if !passableOnly || passable {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

// moderatorLists returns, per server, the union of the lists of all
// moderator groups through which the moderator has that server (ROLE-010),
// limited to the scope of each group's owner (Design 3.2).
func (c *Checker) moderatorLists(ctx context.Context, userID string) (map[string]list, error) {
	q := c.st.Queries
	grants, err := q.ListModeratorGrants(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load grants: %w", err)
	}
	ownerScope := map[string]map[string]bool{}
	entries := map[string]list{}
	out := map[string]list{}
	for _, g := range grants {
		scope, ok := ownerScope[g.OwnerID]
		if !ok {
			if scope, err = c.ownerScope(ctx, g.OwnerID); err != nil {
				return nil, err
			}
			ownerScope[g.OwnerID] = scope
		}
		if !scope[g.ServerID] {
			continue
		}
		l := list{}
		if g.CommandListID.Valid {
			if l, ok = entries[g.CommandListID.String]; !ok {
				rows, err := q.ListCommandListEntries(ctx, g.CommandListID.String)
				if err != nil {
					return nil, fmt.Errorf("load command list: %w", err)
				}
				l = list{}
				for _, e := range rows {
					l[e.Path] = e.Passable == 1
				}
				entries[g.CommandListID.String] = l
			}
		}
		merged := out[g.ServerID]
		if merged == nil {
			merged = list{}
			out[g.ServerID] = merged
		}
		for p, passable := range l {
			merged[p] = merged[p] || passable
		}
	}
	return out, nil
}

// ownerScope is the server set of the owner of a moderator group: an
// active RCON admin or server admin; any other owner grants nothing.
func (c *Checker) ownerScope(ctx context.Context, ownerID string) (map[string]bool, error) {
	o, err := c.st.Queries.GetRightsUser(ctx, ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load group owner: %w", err)
	}
	role := Role(o.Role)
	if o.DisabledAt.Valid || o.DeletedAt.Valid || (role != RCONAdmin && role != ServerAdmin) {
		return map[string]bool{}, nil
	}
	ids, err := c.adminServers(ctx, ownerID, role)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set, nil
}

// supporterRights gives a supporter, per assigned server, the passable
// commands of the list that applies to his moderator there (ROLE-007).
// Nothing applies while the global switch is off or the moderator is not
// allowed to have supporters (ROLE-006).
func (c *Checker) supporterRights(ctx context.Context, userID string, r *Rights) error {
	var enabled bool
	if err := c.st.GlobalSetting(ctx, SettingSupportersEnabled, &enabled); err != nil || !enabled {
		return nil //nolint:nilerr // a missing switch means off
	}
	q := c.st.Queries
	grants, err := q.ListSupporterGrants(ctx, userID)
	if err != nil {
		return fmt.Errorf("load supporter grants: %w", err)
	}
	moderators := map[string]map[string]list{}
	for _, g := range grants {
		lists, ok := moderators[g.ModeratorID]
		if !ok {
			m, err := q.GetRightsUser(ctx, g.ModeratorID)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				lists = nil
			case err != nil:
				return fmt.Errorf("load moderator: %w", err)
			case m.DisabledAt.Valid || m.DeletedAt.Valid || Role(m.Role) != Moderator || m.SupportersAllowed != 1:
				lists = nil
			default:
				if lists, err = c.moderatorLists(ctx, g.ModeratorID); err != nil {
					return err
				}
			}
			moderators[g.ModeratorID] = lists
		}
		l, ok := lists[g.ServerID]
		if !ok {
			continue // the server left the moderator's scope
		}
		s := r.servers[g.ServerID]
		s.Commands = mergeSorted(s.Commands, l.paths(true))
		r.servers[g.ServerID] = s
	}
	return nil
}

func mergeSorted(a, b []string) []string {
	out := slices.Concat(a, b)
	slices.Sort(out)
	return slices.Compact(out)
}
