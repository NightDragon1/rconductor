// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// Group kinds (Design 4.2).
const (
	KindGeneral   = "general"
	KindModerator = "moderator"
)

// maxNameLen bounds names of groups and lists.
const maxNameLen = 64

// Group is a user group or a moderator group with its members and the
// grants that apply to all members (AUTH-002, ROLE-005).
type Group struct {
	ID            string
	Name          string
	Kind          string
	OwnerID       string
	CommandListID string
	Members       []string
	Grants        []Target
}

// GroupUpdate holds the changes of a PATCH; nil fields stay. An empty
// CommandListID removes the list.
type GroupUpdate struct {
	Name          *string
	CommandListID *string
	Members       *[]string
	Grants        *[]Target
	// Version is the version read before (required); the write is refused
	// with ErrConflict if it is stale.
	Version *int64
}

// mayAdministerGroups: RCON admin and server admins (Design 2.2).
func mayAdministerGroups(r perm.Role) bool { return r == perm.RCONAdmin || r == perm.ServerAdmin }

func validName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxNameLen {
		return "", &InvalidError{Detail: fmt.Sprintf("a name has 1 to %d characters", maxNameLen)}
	}
	return name, nil
}

// Groups lists the groups the caller administers: all for the RCON admin,
// his own for a server admin.
func (s *Service) Groups(ctx context.Context, req Request) ([]Group, error) {
	if !mayAdministerGroups(req.Actor.Role) {
		s.deniedOn(ctx, req, "group.list", "user_group", "", "forbidden", nil)
		return nil, ErrForbidden
	}
	rows, err := s.st.Queries.ListGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	out := []Group{}
	for _, r := range rows {
		if req.Actor.Role != perm.RCONAdmin && r.OwnerID != req.Actor.ID {
			continue
		}
		g, err := s.groupDetails(ctx, sqlitedb.GetGroupRow(r))
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

func (s *Service) groupDetails(ctx context.Context, r sqlitedb.GetGroupRow) (Group, error) {
	return groupDetailsOf(ctx, s.st.Queries, r)
}

// groupDetailsOf reads members and grants through q, inside a transaction
// for an update.
func groupDetailsOf(ctx context.Context, q *sqlitedb.Queries, r sqlitedb.GetGroupRow) (Group, error) {
	g := Group{ID: r.ID, Name: r.Name, Kind: r.Kind, OwnerID: r.OwnerID, CommandListID: r.CommandListID.String, Grants: []Target{}}
	var err error
	if g.Members, err = q.ListGroupMembers(ctx, r.ID); err != nil {
		return Group{}, err
	}
	grants, err := q.ListGroupGrants(ctx, r.ID)
	if err != nil {
		return Group{}, err
	}
	for _, gr := range grants {
		g.Grants = append(g.Grants, targetOf(gr.ServerGroupID, gr.ServerID))
	}
	return g, nil
}

// group loads a group the caller administers, else ErrForbidden for a
// role without groups or ErrNotFound (logged).
func (s *Service) group(ctx context.Context, req Request, id, action string) (sqlitedb.GetGroupRow, error) {
	if !mayAdministerGroups(req.Actor.Role) {
		s.deniedOn(ctx, req, action, "user_group", id, "forbidden", nil)
		return sqlitedb.GetGroupRow{}, ErrForbidden
	}
	g, err := s.st.Queries.GetGroup(ctx, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && req.Actor.Role != perm.RCONAdmin && g.OwnerID != req.Actor.ID) {
		s.deniedOn(ctx, req, action, "user_group", id, "not_found", nil)
		return g, ErrNotFound
	}
	return g, err
}

// Group returns a group the caller administers.
func (s *Service) Group(ctx context.Context, req Request, id string) (Group, error) {
	g, err := s.group(ctx, req, id, "group.read")
	if err != nil {
		return Group{}, err
	}
	return s.groupDetails(ctx, g)
}

// checkList accepts a command list of the caller (any for the RCON admin).
func (s *Service) checkList(ctx context.Context, req Request, listID string) error {
	owner, err := s.st.Queries.GetCommandListOwner(ctx, listID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && req.Actor.Role != perm.RCONAdmin && owner != req.Actor.ID) {
		s.deniedOn(ctx, req, "group.update", "command_list", listID, "not_found", nil)
		return ErrNotFound
	}
	return err
}

// CreateGroup creates a group owned by the caller.
func (s *Service) CreateGroup(ctx context.Context, req Request, name, kind, listID string) (Group, error) {
	if !mayAdministerGroups(req.Actor.Role) {
		s.deniedOn(ctx, req, "group.create", "user_group", "", "forbidden", nil)
		return Group{}, ErrForbidden
	}
	name, err := validName(name)
	if err != nil {
		return Group{}, err
	}
	if kind != KindGeneral && kind != KindModerator {
		return Group{}, &InvalidError{Detail: "kind is general or moderator"}
	}
	if listID != "" {
		if kind != KindModerator {
			return Group{}, &InvalidError{Detail: "only moderator groups have a command list"}
		}
		if err := s.checkList(ctx, req, listID); err != nil {
			return Group{}, err
		}
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
	now := store.Now()
	if err := sqlitedb.New(tx).InsertGroup(ctx, sqlitedb.InsertGroupParams{
		ID: id, Name: name, Kind: kind, OwnerID: req.Actor.ID,
		CommandListID: sql.NullString{String: listID, Valid: listID != ""}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Group{}, &InvalidError{Detail: "you already have a group with this name"}
		}
		return Group{}, err
	}
	if err := s.auditOn(ctx, tx, req, "group.create", "user_group", id, map[string]string{"name": name, "kind": kind, "command_list_id": listID}); err != nil {
		return Group{}, err
	}
	if err := tx.Commit(); err != nil {
		return Group{}, err
	}
	return s.Group(ctx, req, id)
}

// UpdateGroup renames a group, sets its list, replaces its members or its
// group-wide grants. Members must be users the caller manages, and a
// moderator group takes only moderators; grants must lie within the
// caller's scope (ROLE-004). Every member whose rights change loses his
// sessions (decision W2).
func (s *Service) UpdateGroup(ctx context.Context, req Request, id string, up GroupUpdate) (Group, error) {
	g, err := s.group(ctx, req, id, "group.update")
	if err != nil {
		return Group{}, err
	}
	var newName string
	if up.Name != nil {
		if newName, err = validName(*up.Name); err != nil {
			return Group{}, err
		}
	}
	var newList sql.NullString
	if up.CommandListID != nil {
		list := sql.NullString{String: *up.CommandListID, Valid: *up.CommandListID != ""}
		newList = list
		if list.Valid {
			if g.Kind != KindModerator {
				return Group{}, &InvalidError{Detail: "only moderator groups have a command list"}
			}
			if err := s.checkList(ctx, req, list.String); err != nil {
				return Group{}, err
			}
		}
	}
	managed, err := s.perm.Managed(ctx, req.Actor)
	if err != nil {
		return Group{}, err
	}
	ck := &checker{s: s, ctx: ctx, req: req, target: id, rights: map[string]*perm.Rights{}, managed: managed}
	if up.Members != nil {
		for _, uid := range *up.Members {
			u, err := s.load(ctx, uid)
			if errors.Is(err, ErrNotFound) || (err == nil && !managed[uid]) {
				s.deniedOn(ctx, req, "group.update", "user", uid, "not_found", map[string]string{"group": id})
				return Group{}, ErrNotFound
			}
			if err != nil {
				return Group{}, err
			}
			if g.Kind == KindModerator && u.Role != perm.Moderator {
				return Group{}, &InvalidError{Detail: "a moderator group takes only moderators"}
			}
		}
	}
	if up.Grants != nil {
		if g.Kind != KindModerator && len(*up.Grants) > 0 {
			return Group{}, &InvalidError{Detail: "only moderator groups have grants"}
		}
		for _, t := range *up.Grants {
			if !t.valid() {
				return Group{}, &InvalidError{Detail: "a grant names exactly one of server_id or server_group_id"}
			}
			for _, who := range []string{req.Actor.ID, g.OwnerID} {
				ok, err := ck.inScope(who, t)
				if err != nil {
					return Group{}, err
				}
				if !ok {
					s.deniedOn(ctx, req, "group.update", "user_group", id, "not_found", map[string]any{"grant": t})
					return Group{}, ErrNotFound
				}
			}
		}
	}

	affected := map[string]bool{}
	changes := map[string]any{}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return Group{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	// Optimistic locking in one statement (revision 2.4 candidate 24);
	// the version is required, checked after role and scope. The group is
	// read after the version, in the transaction: names, list and members
	// are compared with the state the caller read.
	if up.Version == nil {
		return Group{}, ErrPreconditionRequired
	}
	n, err := q.BumpUserGroupVersion(ctx, sqlitedb.BumpUserGroupVersionParams{ID: id, Version: *up.Version})
	if err != nil {
		return Group{}, err
	}
	if n == 0 {
		return Group{}, ErrConflict
	}
	if g, err = q.GetGroup(ctx, id); err != nil {
		return Group{}, err
	}
	before, err := groupDetailsOf(ctx, q, g)
	if err != nil {
		return Group{}, err
	}
	name, list := g.Name, g.CommandListID
	if up.Name != nil {
		name = newName
	}
	if up.CommandListID != nil {
		list = newList
	}
	now := store.Now()
	if name != g.Name || list != g.CommandListID {
		if err := q.UpdateGroup(ctx, sqlitedb.UpdateGroupParams{Name: name, CommandListID: list, UpdatedAt: now, ID: id}); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return Group{}, &InvalidError{Detail: "the owner already has a group with this name"}
			}
			return Group{}, err
		}
		if name != g.Name {
			changes["name"] = name
		}
		if list != g.CommandListID {
			changes["command_list_id"] = list.String
			for _, m := range before.Members {
				affected[m] = true
			}
		}
	}
	if up.Members != nil {
		want := map[string]bool{}
		for _, m := range *up.Members {
			want[m] = true
		}
		var added, removed []string
		for _, m := range before.Members {
			if want[m] {
				continue
			}
			// Only members the caller manages can be removed by him.
			if !managed[m] {
				continue
			}
			if err := q.DeleteMembership(ctx, sqlitedb.DeleteMembershipParams{UserGroupID: id, UserID: m}); err != nil {
				return Group{}, err
			}
			if err := q.DeleteUserGrantsInGroup(ctx, sqlitedb.DeleteUserGrantsInGroupParams{UserGroupID: id, UserID: sql.NullString{String: m, Valid: true}}); err != nil {
				return Group{}, err
			}
			removed = append(removed, m)
			affected[m] = true
		}
		have := map[string]bool{}
		for _, m := range before.Members {
			have[m] = true
		}
		for m := range want {
			if have[m] {
				continue
			}
			if err := q.InsertMembership(ctx, sqlitedb.InsertMembershipParams{UserGroupID: id, UserID: m, AddedBy: req.Actor.ID, CreatedAt: now}); err != nil {
				return Group{}, err
			}
			added = append(added, m)
			affected[m] = true
		}
		if len(added)+len(removed) > 0 {
			changes["members"] = map[string]any{"added": added, "removed": removed}
		}
	}
	if up.Grants != nil {
		want := map[Target]bool{}
		for _, t := range *up.Grants {
			want[t] = true
		}
		rows, err := q.ListGroupGrants(ctx, id)
		if err != nil {
			return Group{}, err
		}
		var added, removed []Target
		have := map[Target]bool{}
		for _, r := range rows {
			t := targetOf(r.ServerGroupID, r.ServerID)
			have[t] = true
			if !want[t] {
				if err := q.DeleteGrant(ctx, r.ID); err != nil {
					return Group{}, err
				}
				removed = append(removed, t)
			}
		}
		for t := range want {
			if have[t] {
				continue
			}
			gid, err := store.NewID()
			if err != nil {
				return Group{}, err
			}
			sg, sv := t.null()
			if err := q.InsertGrant(ctx, sqlitedb.InsertGrantParams{
				ID: gid, UserGroupID: id, ServerGroupID: sg, ServerID: sv, GrantedBy: req.Actor.ID, CreatedAt: now,
			}); err != nil {
				return Group{}, err
			}
			added = append(added, t)
		}
		if len(added)+len(removed) > 0 {
			changes["grants"] = map[string]any{"added": added, "removed": removed}
			for _, m := range before.Members {
				affected[m] = true
			}
		}
	}
	if len(changes) > 0 {
		if err := s.auditOn(ctx, tx, req, "group.update", "user_group", id, changes); err != nil {
			return Group{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Group{}, err
	}
	if len(changes) > 0 {
		if err := s.rightsChanged(ctx, req, "group.update", keys(affected)...); err != nil {
			return Group{}, err
		}
	}
	return s.Group(ctx, req, id)
}

// DeleteGroup soft-deletes a group; its members lose the rights that came
// through it and their sessions (decision W2).
func (s *Service) DeleteGroup(ctx context.Context, req Request, id string) error {
	g, err := s.group(ctx, req, id, "group.delete")
	if err != nil {
		return err
	}
	members, err := s.st.Queries.ListGroupMembers(ctx, id)
	if err != nil {
		return err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := store.Now()
	if err := sqlitedb.New(tx).SoftDeleteGroup(ctx, sqlitedb.SoftDeleteGroupParams{DeletedAt: sql.NullString{String: now, Valid: true}, UpdatedAt: now, ID: id}); err != nil {
		return err
	}
	if err := s.auditOn(ctx, tx, req, "group.delete", "user_group", id, map[string]string{"name": g.Name}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if g.Kind != KindModerator {
		s.perm.Invalidate()
		return nil
	}
	return s.rightsChanged(ctx, req, "group.delete", members...)
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
