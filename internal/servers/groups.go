// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Group is a server group with its servers (SRV-003). A server can be in
// several groups.
type Group struct {
	ID        string
	Name      string
	ServerIDs []string
}

func (s *Service) groupMembers(ctx context.Context) (map[string][]string, error) {
	rows, err := s.st.Queries.ListServerGroupMemberships(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, r := range rows {
		out[r.ServerGroupID] = append(out[r.ServerGroupID], r.ServerID)
	}
	return out, nil
}

// groupInScope: the RCON admin sees every group; a server admin sees a
// group only if it is not empty and all its servers are in his scope.
func (s *Service) groupInScope(ctx context.Context, req users.Request, id string) (bool, error) {
	if req.Actor.Role == perm.RCONAdmin {
		return true, nil
	}
	if req.Actor.Role != perm.ServerAdmin {
		return false, nil
	}
	rights, err := s.perm.For(ctx, req.Actor.ID)
	if err != nil {
		return false, err
	}
	members, err := s.groupMembers(ctx)
	if err != nil {
		return false, err
	}
	if len(members[id]) == 0 {
		return false, nil
	}
	for _, sid := range members[id] {
		if _, ok := rights.Server(sid); !ok {
			return false, nil
		}
	}
	return true, nil
}

// Groups lists the server groups the caller may see (Design 2.2: RCON
// admin all, server admin readable in his scope).
func (s *Service) Groups(ctx context.Context, req users.Request) ([]Group, error) {
	rows, err := s.st.Queries.ListServerGroups(ctx)
	if err != nil {
		return nil, err
	}
	members, err := s.groupMembers(ctx)
	if err != nil {
		return nil, err
	}
	out := []Group{}
	for _, r := range rows {
		ok, err := s.groupInScope(ctx, req, r.ID)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, Group{ID: r.ID, Name: r.Name, ServerIDs: nonNil(members[r.ID])})
		}
	}
	return out, nil
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// Group returns a server group the caller may see.
func (s *Service) Group(ctx context.Context, req users.Request, id string) (Group, error) {
	r, err := s.st.Queries.GetServerGroup(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Group{}, users.ErrNotFound
	}
	if err != nil {
		return Group{}, err
	}
	ok, err := s.groupInScope(ctx, req, id)
	if err != nil {
		return Group{}, err
	}
	if !ok {
		s.denied(ctx, req, "server_group.read", "server_group", id, "not_found")
		return Group{}, users.ErrNotFound
	}
	members, err := s.groupMembers(ctx)
	if err != nil {
		return Group{}, err
	}
	return Group{ID: r.ID, Name: r.Name, ServerIDs: nonNil(members[id])}, nil
}

func (s *Service) checkServers(ctx context.Context, ids []string) ([]string, error) {
	out := slices.Clone(ids)
	slices.Sort(out)
	out = slices.Compact(out)
	for _, id := range out {
		if _, err := s.st.Queries.GetServer(ctx, id); err != nil {
			return nil, &users.InvalidError{Detail: "unknown server " + id}
		}
	}
	return out, nil
}

func writeMembers(ctx context.Context, q *sqlitedb.Queries, group string, servers []string) error {
	if err := q.DeleteServerGroupMembers(ctx, group); err != nil {
		return err
	}
	for _, sid := range servers {
		if err := q.InsertServerGroupMember(ctx, sqlitedb.InsertServerGroupMemberParams{ServerGroupID: group, ServerID: sid}); err != nil {
			return err
		}
	}
	return nil
}

// CreateGroup adds a server group; RCON admin only (SRV-003).
func (s *Service) CreateGroup(ctx context.Context, req users.Request, name string, servers []string) (Group, error) {
	if err := s.mustBeAdmin(ctx, req, "server_group.create", "server_group", ""); err != nil {
		return Group{}, err
	}
	name, err := plainName(name)
	if err != nil {
		return Group{}, err
	}
	if servers, err = s.checkServers(ctx, servers); err != nil {
		return Group{}, err
	}
	id, err := store.NewID()
	if err != nil {
		return Group{}, err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return Group{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	now := store.Now()
	if err := q.InsertServerGroup(ctx, sqlitedb.InsertServerGroupParams{ID: id, Name: name, CreatedAt: now, UpdatedAt: now}); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Group{}, &users.InvalidError{Detail: "a server group with this name exists"}
		}
		return Group{}, err
	}
	if err := writeMembers(ctx, q, id, servers); err != nil {
		return Group{}, err
	}
	if err := s.auditOK(ctx, tx, req, "server_group.create", "server_group", id, "", map[string]any{"name": name, "servers": servers}); err != nil {
		return Group{}, err
	}
	if err := tx.Commit(); err != nil {
		return Group{}, err
	}
	s.perm.Invalidate()
	return s.Group(ctx, req, id)
}

// UpdateGroup renames a group or replaces its servers. Scopes and grants on
// the group follow at once; sessions stay, because nobody's own
// assignments changed.
func (s *Service) UpdateGroup(ctx context.Context, req users.Request, id string, name *string, servers *[]string) (Group, error) {
	if err := s.mustBeAdmin(ctx, req, "server_group.update", "server_group", id); err != nil {
		return Group{}, err
	}
	r, err := s.st.Queries.GetServerGroup(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Group{}, users.ErrNotFound
	}
	if err != nil {
		return Group{}, err
	}
	newName := r.Name
	if name != nil {
		if newName, err = plainName(*name); err != nil {
			return Group{}, err
		}
	}
	var members []string
	if servers != nil {
		if members, err = s.checkServers(ctx, *servers); err != nil {
			return Group{}, err
		}
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return Group{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	changes := map[string]any{}
	if newName != r.Name {
		if err := q.RenameServerGroup(ctx, sqlitedb.RenameServerGroupParams{Name: newName, UpdatedAt: store.Now(), ID: id}); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return Group{}, &users.InvalidError{Detail: "a server group with this name exists"}
			}
			return Group{}, err
		}
		changes["name"] = newName
	}
	if servers != nil {
		if err := writeMembers(ctx, q, id, members); err != nil {
			return Group{}, err
		}
		changes["servers"] = members
	}
	if len(changes) > 0 {
		if err := s.auditOK(ctx, tx, req, "server_group.update", "server_group", id, "", changes); err != nil {
			return Group{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Group{}, err
	}
	s.perm.Invalidate()
	return s.Group(ctx, req, id)
}

// DeleteGroup soft-deletes a server group; scopes and grants on it end.
func (s *Service) DeleteGroup(ctx context.Context, req users.Request, id string) error {
	if err := s.mustBeAdmin(ctx, req, "server_group.delete", "server_group", id); err != nil {
		return err
	}
	r, err := s.st.Queries.GetServerGroup(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return users.ErrNotFound
	}
	if err != nil {
		return err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := store.Now()
	if err := sqlitedb.New(tx).SoftDeleteServerGroup(ctx, sqlitedb.SoftDeleteServerGroupParams{DeletedAt: sql.NullString{String: now, Valid: true}, UpdatedAt: now, ID: id}); err != nil {
		return err
	}
	if err := s.auditOK(ctx, tx, req, "server_group.delete", "server_group", id, "", map[string]string{"name": r.Name}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.perm.Invalidate()
	return nil
}
