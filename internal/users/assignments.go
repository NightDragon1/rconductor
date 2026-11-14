// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// Target is exactly one of a server or a server group.
type Target struct {
	ServerID      string
	ServerGroupID string
}

func (t Target) valid() bool { return (t.ServerID == "") != (t.ServerGroupID == "") }

func targetOf(group, server sql.NullString) Target {
	return Target{ServerID: server.String, ServerGroupID: group.String}
}

func (t Target) null() (group, server sql.NullString) {
	return sql.NullString{String: t.ServerGroupID, Valid: t.ServerGroupID != ""},
		sql.NullString{String: t.ServerID, Valid: t.ServerID != ""}
}

// Grant gives a moderator a server or server group through one of his
// moderator groups, which supplies the command list (Design 4.3).
type Grant struct {
	GroupID string
	Target
}

// SupporterGrant gives a supporter a server of his moderator.
type SupporterGrant struct {
	ModeratorID string
	ServerID    string
}

// Assignments are role, scopes, groups and grants of one user; they also
// serve as template for quick entry (UI-008).
type Assignments struct {
	Role            perm.Role
	AdminScopes     []Target
	Groups          []string
	Grants          []Grant
	SupporterGrants []SupporterGrant
}

// AssignmentsUpdate replaces the given sections; nil sections stay. Within
// a section only entries the caller controls are replaced; entries made by
// others in their own groups stay.
type AssignmentsUpdate struct {
	AdminScopes     *[]Target
	Groups          *[]string
	Grants          *[]Grant
	SupporterGrants *[]SupporterGrant
}

type current struct {
	scopes     []sqlitedb.ListUserScopesRow
	members    []sqlitedb.ListUserMembershipsRow
	grants     []sqlitedb.ListUserDirectGrantsRow
	supporters []sqlitedb.ListUserSupporterGrantsRow
}

func (s *Service) loadCurrent(ctx context.Context, q *sqlitedb.Queries, id string) (current, error) {
	var c current
	var err error
	if c.scopes, err = q.ListUserScopes(ctx, id); err != nil {
		return c, err
	}
	if c.members, err = q.ListUserMemberships(ctx, id); err != nil {
		return c, err
	}
	if c.grants, err = q.ListUserDirectGrants(ctx, sql.NullString{String: id, Valid: true}); err != nil {
		return c, err
	}
	c.supporters, err = q.ListUserSupporterGrants(ctx, id)
	return c, err
}

// Assignments returns the assignments of a managed user.
func (s *Service) Assignments(ctx context.Context, req Request, id string) (Assignments, error) {
	if err := s.mustManage(ctx, req, id, "assignments.read"); err != nil {
		return Assignments{}, err
	}
	return s.assignments(ctx, id)
}

// assignments reads the assignments without a permission check; the
// answer to an update uses it, since the caller may have just given up
// managing the user.
func (s *Service) assignments(ctx context.Context, id string) (Assignments, error) {
	u, err := s.load(ctx, id)
	if err != nil {
		return Assignments{}, err
	}
	c, err := s.loadCurrent(ctx, s.st.Queries, id)
	if err != nil {
		return Assignments{}, fmt.Errorf("load assignments: %w", err)
	}
	a := Assignments{Role: u.Role, AdminScopes: []Target{}, Groups: []string{}, Grants: []Grant{}, SupporterGrants: []SupporterGrant{}}
	for _, r := range c.scopes {
		a.AdminScopes = append(a.AdminScopes, targetOf(r.ServerGroupID, r.ServerID))
	}
	for _, r := range c.members {
		a.Groups = append(a.Groups, r.ID)
	}
	for _, r := range c.grants {
		a.Grants = append(a.Grants, Grant{GroupID: r.UserGroupID, Target: targetOf(r.ServerGroupID, r.ServerID)})
	}
	for _, r := range c.supporters {
		a.SupporterGrants = append(a.SupporterGrants, SupporterGrant{ModeratorID: r.ModeratorID, ServerID: r.ServerID})
	}
	return a, nil
}

// checker bundles what the validation of one request needs.
type checker struct {
	s       *Service
	ctx     context.Context
	req     Request
	target  string
	rights  map[string]*perm.Rights
	managed map[string]bool
	// q reads in the writing transaction once it is open: a target must
	// still exist when the row that names it is written.
	q *sqlitedb.Queries
	// denials are written after the transaction has ended, because the
	// open write transaction holds the database lock.
	denials []func()
}

func (c *checker) rightsOf(id string) (*perm.Rights, error) {
	if r, ok := c.rights[id]; ok {
		return r, nil
	}
	r, err := c.s.perm.For(c.ctx, id)
	if err != nil {
		return nil, err
	}
	c.rights[id] = r
	return r, nil
}

// refuse records a refused entry for log and audit and returns err.
func (c *checker) refuse(err error, reason string, entry any) error {
	c.denials = append(c.denials, func() {
		c.s.denied(c.ctx, c.req, "assignments.update", c.target, reason, map[string]any{"entry": entry})
	})
	return err
}

// inScope reports whether a server or a whole server group lies within the
// rights of user id; unknown targets are never in scope.
func (c *checker) inScope(id string, t Target) (bool, error) {
	r, err := c.rightsOf(id)
	if err != nil {
		return false, err
	}
	q := c.s.st.Queries
	if c.q != nil {
		q = c.q
	}
	if t.ServerID != "" {
		if _, ok := r.Server(t.ServerID); !ok {
			return false, nil
		}
		n, err := q.ServerExists(c.ctx, t.ServerID)
		return err == nil && n > 0, err
	}
	n, err := q.ServerGroupExists(c.ctx, t.ServerGroupID)
	if err != nil || n == 0 {
		return false, err
	}
	ids, err := q.ListServerGroupServerIDs(c.ctx, t.ServerGroupID)
	if err != nil {
		return false, err
	}
	for _, sid := range ids {
		if _, ok := r.Server(sid); !ok {
			return false, nil
		}
	}
	return true, nil
}

// controlsGroup reports whether the caller administers a group: its owner
// or the RCON admin.
func (c *checker) controlsGroup(owner string) bool {
	return c.req.Actor.Role == perm.RCONAdmin || owner == c.req.Actor.ID
}

// controlsModerator reports whether the caller may handle supporters of a
// moderator: the moderator himself or whoever manages him.
func (c *checker) controlsModerator(id string) bool {
	return id == c.req.Actor.ID || c.managed[id]
}

// UpdateAssignments replaces sections of a managed user's assignments.
func (s *Service) UpdateAssignments(ctx context.Context, req Request, id string, up AssignmentsUpdate) (Assignments, error) {
	if err := s.mustManage(ctx, req, id, "assignments.update"); err != nil {
		return Assignments{}, err
	}
	u, err := s.load(ctx, id)
	if err != nil {
		return Assignments{}, err
	}
	managed, err := s.perm.Managed(ctx, req.Actor)
	if err != nil {
		return Assignments{}, err
	}
	ck := &checker{s: s, ctx: ctx, req: req, target: id, rights: map[string]*perm.Rights{}, managed: managed}
	cur, err := s.loadCurrent(ctx, s.st.Queries, id)
	if err != nil {
		return Assignments{}, fmt.Errorf("load assignments: %w", err)
	}

	changes, err := s.applyAssignments(ck, u, cur, up)
	for _, d := range ck.denials {
		d()
	}
	if err != nil {
		return Assignments{}, err
	}
	if _, scope := changes["admin_scopes"]; scope && len(changes) == 1 {
		// The scope alone ends no session (D-044): rights are checked on
		// each request, and dropping the cache checks open sockets again.
		s.perm.Invalidate()
	} else if len(changes) > 0 {
		if err := s.rightsChanged(ctx, req, "assignments.update", id); err != nil {
			return Assignments{}, err
		}
	}
	return s.assignments(ctx, id)
}

// applyAssignments writes all sections in one transaction.
func (s *Service) applyAssignments(ck *checker, u User, cur current, up AssignmentsUpdate) (map[string]any, error) {
	tx, err := s.st.DB.BeginTx(ck.ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	ck.q = q
	changes := map[string]any{}
	now := store.Now()

	if up.AdminScopes != nil {
		if err := s.applyScopes(ck, q, u, cur, *up.AdminScopes, now, changes); err != nil {
			return nil, err
		}
	}
	members := map[string]bool{}
	for _, m := range cur.members {
		members[m.ID] = true
	}
	if up.Groups != nil {
		if members, err = s.applyGroups(ck, q, u, cur, *up.Groups, now, changes); err != nil {
			return nil, err
		}
	}
	if up.Grants != nil {
		if err := s.applyGrants(ck, q, u, cur, members, *up.Grants, now, changes); err != nil {
			return nil, err
		}
	}
	if up.SupporterGrants != nil {
		if err := s.applySupporterGrants(ck, q, u, cur, *up.SupporterGrants, now, changes); err != nil {
			return nil, err
		}
	}
	if len(changes) > 0 {
		if err := s.audit(ck.ctx, tx, ck.req, "assignments.update", u.ID, changes); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("update assignments: %w", err)
	}
	return changes, nil
}

// applyScopes sets the scope of a server admin; only the RCON admin assigns
// scopes, and a server admin cannot change his own (ROLE-003).
func (s *Service) applyScopes(ck *checker, q *sqlitedb.Queries, u User, cur current, want []Target, now string, changes map[string]any) error {
	if ck.req.Actor.Role != perm.RCONAdmin || u.Role != perm.ServerAdmin {
		if len(want) == 0 && len(cur.scopes) == 0 {
			return nil
		}
		return ck.refuse(ErrForbidden, "forbidden", map[string]any{"admin_scopes": want})
	}
	wanted := map[Target]bool{}
	for _, t := range want {
		if !t.valid() {
			return &InvalidError{Detail: "a scope names exactly one of server_id or server_group_id"}
		}
		ok, err := ck.inScope(ck.req.Actor.ID, t)
		if err != nil {
			return err
		}
		if !ok {
			return ck.refuse(ErrNotFound, "not_found", t)
		}
		wanted[t] = true
	}
	var added, removed []Target
	have := map[Target]bool{}
	for _, r := range cur.scopes {
		t := targetOf(r.ServerGroupID, r.ServerID)
		have[t] = true
		if !wanted[t] {
			if err := q.DeleteAdminScope(ck.ctx, r.ID); err != nil {
				return err
			}
			removed = append(removed, t)
		}
	}
	for t := range wanted {
		if have[t] {
			continue
		}
		id, err := store.NewID()
		if err != nil {
			return err
		}
		g, sv := t.null()
		if err := q.InsertAdminScope(ck.ctx, sqlitedb.InsertAdminScopeParams{
			ID: id, UserID: u.ID, ServerGroupID: g, ServerID: sv, AssignedBy: ck.req.Actor.ID, CreatedAt: now,
		}); err != nil {
			return err
		}
		added = append(added, t)
	}
	if len(added)+len(removed) > 0 {
		changes["admin_scopes"] = map[string]any{"added": added, "removed": removed}
	}
	return nil
}

// applyGroups sets memberships in the groups the caller controls and
// returns the resulting membership set. Moderator groups take only
// moderators. Leaving a group removes the user's direct grants in it.
func (s *Service) applyGroups(ck *checker, q *sqlitedb.Queries, u User, cur current, want []string, now string, changes map[string]any) (map[string]bool, error) {
	wanted := map[string]bool{}
	for _, gid := range want {
		g, err := q.GetActiveGroup(ck.ctx, gid)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && !ck.controlsGroup(g.OwnerID)) {
			return nil, ck.refuse(ErrNotFound, "not_found", map[string]string{"group": gid})
		}
		if err != nil {
			return nil, err
		}
		if g.Kind == "moderator" && u.Role != perm.Moderator {
			return nil, &InvalidError{Detail: "only moderators can join a moderator group"}
		}
		wanted[gid] = true
	}
	result := map[string]bool{}
	var added, removed []string
	for _, m := range cur.members {
		if !ck.controlsGroup(m.OwnerID) || wanted[m.ID] {
			result[m.ID] = true
			continue
		}
		if err := q.DeleteMembership(ck.ctx, sqlitedb.DeleteMembershipParams{UserGroupID: m.ID, UserID: u.ID}); err != nil {
			return nil, err
		}
		for _, g := range cur.grants {
			if g.UserGroupID == m.ID {
				if err := q.DeleteGrant(ck.ctx, g.ID); err != nil {
					return nil, err
				}
			}
		}
		removed = append(removed, m.ID)
	}
	for gid := range wanted {
		if result[gid] {
			continue
		}
		if err := q.InsertMembership(ck.ctx, sqlitedb.InsertMembershipParams{
			UserGroupID: gid, UserID: u.ID, AddedBy: ck.req.Actor.ID, CreatedAt: now,
		}); err != nil {
			return nil, err
		}
		result[gid] = true
		added = append(added, gid)
	}
	if len(added)+len(removed) > 0 {
		changes["groups"] = map[string]any{"added": added, "removed": removed}
	}
	return result, nil
}

// applyGrants sets the direct grants of a moderator in the moderator
// groups the caller controls. The moderator must be a member of the
// group, and the target must lie within the caller's scope and the group
// owner's scope (ROLE-004).
func (s *Service) applyGrants(ck *checker, q *sqlitedb.Queries, u User, cur current, members map[string]bool, want []Grant, now string, changes map[string]any) error {
	if u.Role != perm.Moderator {
		if len(want) == 0 {
			return nil
		}
		return &InvalidError{Detail: "grants are only for moderators"}
	}
	wanted := map[Grant]bool{}
	for _, g := range want {
		if !g.valid() {
			return &InvalidError{Detail: "a grant names exactly one of server_id or server_group_id"}
		}
		grp, err := q.GetActiveGroup(ck.ctx, g.GroupID)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && (!ck.controlsGroup(grp.OwnerID) || grp.Kind != "moderator")) {
			return ck.refuse(ErrNotFound, "not_found", g)
		}
		if err != nil {
			return err
		}
		if !members[g.GroupID] {
			return &InvalidError{Detail: "the moderator must be a member of the group of a grant"}
		}
		for _, who := range []string{ck.req.Actor.ID, grp.OwnerID} {
			ok, err := ck.inScope(who, g.Target)
			if err != nil {
				return err
			}
			if !ok {
				return ck.refuse(ErrNotFound, "not_found", g)
			}
		}
		wanted[g] = true
	}
	var added, removed []Grant
	have := map[Grant]bool{}
	for _, r := range cur.grants {
		g := Grant{GroupID: r.UserGroupID, Target: targetOf(r.ServerGroupID, r.ServerID)}
		have[g] = true
		if !ck.controlsGroup(r.OwnerID) || wanted[g] {
			continue
		}
		if err := q.DeleteGrant(ck.ctx, r.ID); err != nil {
			return err
		}
		removed = append(removed, g)
	}
	for g := range wanted {
		if have[g] {
			continue
		}
		id, err := store.NewID()
		if err != nil {
			return err
		}
		sg, sv := g.null()
		if err := q.InsertGrant(ck.ctx, sqlitedb.InsertGrantParams{
			ID: id, UserGroupID: g.GroupID, UserID: sql.NullString{String: u.ID, Valid: true},
			ServerGroupID: sg, ServerID: sv, GrantedBy: ck.req.Actor.ID, CreatedAt: now,
		}); err != nil {
			return err
		}
		added = append(added, g)
	}
	if len(added)+len(removed) > 0 {
		changes["grants"] = map[string]any{"added": added, "removed": removed}
	}
	return nil
}

// applySupporterGrants sets the servers of a supporter for the moderators
// the caller controls. Supporters must be enabled globally and for the
// moderator (ROLE-006), and the server must be in the moderator's scope.
func (s *Service) applySupporterGrants(ck *checker, q *sqlitedb.Queries, u User, cur current, want []SupporterGrant, now string, changes map[string]any) error {
	if u.Role != perm.Supporter {
		if len(want) == 0 {
			return nil
		}
		return &InvalidError{Detail: "supporter grants are only for supporters"}
	}
	var enabled bool
	if err := s.st.GlobalSetting(ck.ctx, perm.SettingSupportersEnabled, &enabled); err != nil {
		enabled = false
	}
	wanted := map[SupporterGrant]bool{}
	for _, g := range want {
		if !ck.controlsModerator(g.ModeratorID) {
			return ck.refuse(ErrNotFound, "not_found", g)
		}
		m, err := s.load(ck.ctx, g.ModeratorID)
		if errors.Is(err, ErrNotFound) || (err == nil && m.Role != perm.Moderator) {
			return ck.refuse(ErrNotFound, "not_found", g)
		}
		if err != nil {
			return err
		}
		if !enabled || !m.SupportersAllowed {
			return ck.refuse(ErrForbidden, "forbidden", g)
		}
		ok, err := ck.inScope(g.ModeratorID, Target{ServerID: g.ServerID})
		if err != nil {
			return err
		}
		if !ok {
			return ck.refuse(ErrNotFound, "not_found", g)
		}
		wanted[g] = true
	}
	var added, removed []SupporterGrant
	have := map[SupporterGrant]bool{}
	for _, r := range cur.supporters {
		g := SupporterGrant{ModeratorID: r.ModeratorID, ServerID: r.ServerID}
		have[g] = true
		if !ck.controlsModerator(r.ModeratorID) || wanted[g] {
			continue
		}
		if err := q.DeleteSupporterGrant(ck.ctx, r.ID); err != nil {
			return err
		}
		removed = append(removed, g)
	}
	for g := range wanted {
		if have[g] {
			continue
		}
		id, err := store.NewID()
		if err != nil {
			return err
		}
		if err := q.InsertSupporterGrant(ck.ctx, sqlitedb.InsertSupporterGrantParams{
			ID: id, SupporterID: u.ID, ModeratorID: g.ModeratorID, ServerID: g.ServerID,
			GrantedBy: ck.req.Actor.ID, CreatedAt: now,
		}); err != nil {
			return err
		}
		added = append(added, g)
	}
	if len(added)+len(removed) > 0 {
		changes["supporter_grants"] = map[string]any{"added": added, "removed": removed}
	}
	return nil
}
