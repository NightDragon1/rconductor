// SPDX-License-Identifier: AGPL-3.0-or-later

// Package users is the administration of users and their assignments
// (AUTH-001, AUTH-002, ROLE-003, ROLE-004, ROLE-008). Every decision about
// who may change what comes from the permission module.
package users

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/features"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

var (
	// ErrNotFound means the user does not exist or is outside the
	// caller's scope; both look the same (Design 3.1).
	ErrNotFound = errors.New("not found")
	// ErrForbidden means the caller may see the user but not make this
	// change, for example raise a role to his own level.
	ErrForbidden = errors.New("not allowed")
	// ErrConflict means the object changed since the caller read it
	// (revision 2.4 candidate 24).
	ErrConflict = errors.New("changed by someone else")
	// ErrPreconditionRequired means a versioned write named no version.
	ErrPreconditionRequired = errors.New("the version read before is required")
)

// InvalidError is a request that breaks a rule of the data.
type InvalidError struct{ Detail string }

func (e *InvalidError) Error() string { return e.Detail }

// Request identifies the caller of an administration request.
type Request struct {
	Actor      perm.Actor
	Pseudonym  string
	SessionRef string
	SessionID  string // session hash for the audit
	RequestID  string
	// BatchID ties the runs of one multi-server command together (CMD-010,
	// D-069); empty for everything else. The panel makes it.
	BatchID string
}

// SessionRevoker ends the sessions of a user (auth.Service.RevokeUser).
type SessionRevoker func(ctx context.Context, userID, reason, requestID string) error

// Service administers users.
type Service struct {
	st     *store.Store
	perm   *perm.Checker
	log    *slog.Logger
	revoke SessionRevoker
	// features are the flags whose settings keys the RCON admin may
	// change (NFR-019).
	features *features.Set
}

// NewService returns the user administration.
func NewService(st *store.Store, c *perm.Checker, log *slog.Logger, revoke SessionRevoker) *Service {
	return &Service{st: st, perm: c, log: log, revoke: revoke}
}

// UseFeatures sets the feature flags whose keys UpdateSettings accepts.
func (s *Service) UseFeatures(f *features.Set) { s.features = f }

// User is a user as shown to someone who manages him (decision W4).
type User struct {
	ID                string
	Pseudonym         string
	Role              perm.Role
	RealName          string
	Contacts          map[string]string
	HasAvatar         bool
	Disabled          bool
	IsEmergency       bool
	SupportersAllowed bool
	CreatedAt         time.Time
	LastLoginAt       *time.Time
	// TOTPEnabled and EmailSet are what managers see of the second factor
	// and of the address, never the address itself (AUTH-008).
	TOTPEnabled bool
	EmailSet    bool
}

func toUser(r sqlitedb.ListUsersRow) User {
	u := User{
		ID: r.ID, Pseudonym: r.Pseudonym, Role: perm.Role(r.Role), RealName: r.RealName.String,
		Contacts: map[string]string{}, HasAvatar: r.HasAvatar == 1, Disabled: r.DisabledAt.Valid,
		IsEmergency: r.IsEmergency == 1, SupportersAllowed: r.SupportersAllowed == 1,
		TOTPEnabled: r.TotpEnabled == 1, EmailSet: r.EmailSet == 1,
	}
	_ = json.Unmarshal([]byte(r.Contacts), &u.Contacts)
	u.CreatedAt, _ = time.Parse(store.TimeLayout, r.CreatedAt)
	if r.LastLoginAt.Valid {
		if t, err := time.Parse(store.TimeLayout, r.LastLoginAt.String); err == nil {
			u.LastLoginAt = &t
		}
	}
	return u
}

// Filter narrows List.
type Filter struct {
	Role  perm.Role // empty: all
	Query string    // part of pseudonym or real name, ignoring case
}

// List returns the users the caller manages.
func (s *Service) List(ctx context.Context, req Request, f Filter) ([]User, error) {
	if err := s.mayManageUsers(ctx, req, "user.list"); err != nil {
		return nil, err
	}
	managed, err := s.perm.Managed(ctx, req.Actor)
	if err != nil {
		return nil, err
	}
	rows, err := s.st.Queries.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	q := strings.ToLower(f.Query)
	out := []User{}
	for _, r := range rows {
		if !managed[r.ID] || (f.Role != "" && perm.Role(r.Role) != f.Role) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(r.Pseudonym), q) && !strings.Contains(strings.ToLower(r.RealName.String), q) {
			continue
		}
		out = append(out, toUser(r))
	}
	return out, nil
}

// Get returns a managed user.
func (s *Service) Get(ctx context.Context, req Request, id string) (User, error) {
	if err := s.mustManage(ctx, req, id, "user.read"); err != nil {
		return User{}, err
	}
	return s.load(ctx, id)
}

func (s *Service) load(ctx context.Context, id string) (User, error) {
	r, err := s.st.Queries.GetUser(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("load user: %w", err)
	}
	return toUser(sqlitedb.ListUsersRow(r)), nil
}

// mayManageUsers rejects callers without user administration with
// ErrForbidden, logged as perm.denied and audited (Design 2.2, 3.6).
func (s *Service) mayManageUsers(ctx context.Context, req Request, action string) error {
	ok, err := s.perm.MayManageUsers(ctx, req.Actor)
	if err != nil {
		return err
	}
	if !ok {
		s.deniedOn(ctx, req, action, "user", "", "forbidden", nil)
		return ErrForbidden
	}
	return nil
}

// mustManage rejects callers without user administration with
// ErrForbidden and targets outside the caller's scope with ErrNotFound,
// logged as perm.denied and audited (ROLE-004, ROLE-008).
func (s *Service) mustManage(ctx context.Context, req Request, target, action string) error {
	if err := s.mayManageUsers(ctx, req, action); err != nil {
		return err
	}
	ok, err := s.perm.Manages(ctx, req.Actor, target)
	if err != nil {
		return err
	}
	if !ok {
		s.denied(ctx, req, action, target, "not_found", nil)
		return ErrNotFound
	}
	return nil
}

func (s *Service) denied(ctx context.Context, req Request, action, target, reason string, payload any) {
	s.deniedOn(ctx, req, action, "user", target, reason, payload)
}

// deniedOn logs perm.denied and audits a refused action on any object.
func (s *Service) deniedOn(ctx context.Context, req Request, action, targetType, target, reason string, payload any) {
	perm.LogDenied(s.log, perm.Denial{
		RequestID: req.RequestID, SessionRef: req.SessionRef, User: req.Pseudonym,
		Role: req.Actor.Role, Reason: reason, Action: action,
	})
	if _, err := audit.Write(ctx, s.st.DB, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		TargetType: targetType, TargetID: target, Action: action, Payload: payload,
		Result: audit.ResultDenied, Reason: reason,
	}); err != nil {
		s.log.Error("audit entry not written", slog.String("action", action), slog.Any("err", err))
	}
}

// Update holds the changes of a PATCH; nil fields stay.
type Update struct {
	Role     *perm.Role
	Disabled *bool
	// SupportersAllowed releases supporters for a moderator; only a server
	// admin or the RCON admin sets it (ROLE-006).
	SupportersAllowed *bool
}

// Update changes role or lock state of a managed user. A new role must
// stay below the caller's (ROLE-008); it drops the assignments of the old
// role. Locking ends the user's sessions.
func (s *Service) Update(ctx context.Context, req Request, id string, u Update) (User, error) {
	if err := s.mustManage(ctx, req, id, "user.update"); err != nil {
		return User{}, err
	}
	cur, err := s.load(ctx, id)
	if err != nil {
		return User{}, err
	}
	if u.Role != nil && (!u.Role.Valid() || !req.Actor.Role.Above(*u.Role)) {
		s.denied(ctx, req, "user.update", id, "forbidden", map[string]string{"role": string(*u.Role)})
		return User{}, ErrForbidden
	}

	affected := []string{id}
	if u.SupportersAllowed != nil && *u.SupportersAllowed != cur.SupportersAllowed {
		if cur.Role != perm.Moderator || (req.Actor.Role != perm.ServerAdmin && req.Actor.Role != perm.RCONAdmin) {
			s.denied(ctx, req, "user.update", id, "forbidden", map[string]bool{"supporters_allowed": *u.SupportersAllowed})
			return User{}, ErrForbidden
		}
		// Switching off takes the rights of the moderator's supporters.
		sups, err := s.st.Queries.ListSupportersOfModerator(ctx, id)
		if err != nil {
			return User{}, err
		}
		affected = append(affected, sups...)
	}

	changes := map[string]any{}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	now := store.Now()
	if u.SupportersAllowed != nil && *u.SupportersAllowed != cur.SupportersAllowed {
		v := int64(0)
		if *u.SupportersAllowed {
			v = 1
		}
		if err := q.SetSupportersAllowed(ctx, sqlitedb.SetSupportersAllowedParams{SupportersAllowed: v, UpdatedAt: now, ID: id}); err != nil {
			return User{}, err
		}
		changes["supporters_allowed"] = *u.SupportersAllowed
	}
	if u.Role != nil && *u.Role != cur.Role {
		if err := q.SetUserRole(ctx, sqlitedb.SetUserRoleParams{Role: string(*u.Role), UpdatedAt: now, ID: id}); err != nil {
			return User{}, err
		}
		if err := clearAssignments(ctx, q, id); err != nil {
			return User{}, err
		}
		changes["role"] = map[string]string{"from": string(cur.Role), "to": string(*u.Role)}
	}
	if u.Disabled != nil && *u.Disabled != cur.Disabled {
		at := sql.NullString{String: now, Valid: *u.Disabled}
		if err := q.SetUserDisabled(ctx, sqlitedb.SetUserDisabledParams{DisabledAt: at, UpdatedAt: now, ID: id}); err != nil {
			return User{}, err
		}
		changes["disabled"] = *u.Disabled
	}
	if len(changes) == 0 {
		return cur, nil
	}
	if err := s.audit(ctx, tx, req, "user.update", id, changes); err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, fmt.Errorf("update user: %w", err)
	}
	if err := s.rightsChanged(ctx, req, "user.update", affected...); err != nil {
		return User{}, err
	}
	return s.load(ctx, id)
}

// Delete soft-deletes a managed user and ends his sessions. The record
// stays for the audit trail until the retention period ends (D-028).
func (s *Service) Delete(ctx context.Context, req Request, id string) error {
	if err := s.mustManage(ctx, req, id, "user.delete"); err != nil {
		return err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	now := store.Now()
	if err := q.SoftDeleteUser(ctx, sqlitedb.SoftDeleteUserParams{DeletedAt: sql.NullString{String: now, Valid: true}, UpdatedAt: now, ID: id}); err != nil {
		return err
	}
	if err := clearAssignments(ctx, q, id); err != nil {
		return err
	}
	if err := s.audit(ctx, tx, req, "user.delete", id, nil); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return s.rightsChanged(ctx, req, "user.delete", id)
}

// EndSessions ends all sessions of a managed user (AUTH-006, revision 2.4
// candidate 21); the revoker logs auth.session.revoked and closes his
// sockets.
func (s *Service) EndSessions(ctx context.Context, req Request, id string) error {
	if err := s.mustManage(ctx, req, id, "user.sessions.end"); err != nil {
		return err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.audit(ctx, tx, req, "user.sessions.end", id, nil); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("end sessions: %w", err)
	}
	if s.revoke == nil {
		return nil
	}
	return s.revoke(ctx, id, "user.sessions.end", req.RequestID)
}

func clearAssignments(ctx context.Context, q *sqlitedb.Queries, id string) error {
	grants := func(ctx context.Context, id string) error {
		return q.DeleteUserGrants(ctx, sql.NullString{String: id, Valid: true})
	}
	for _, f := range []func(context.Context, string) error{
		q.DeleteUserScopes, q.DeleteUserMemberships, grants, q.DeleteUserSupporterGrants,
	} {
		if err := f(ctx, id); err != nil {
			return fmt.Errorf("remove assignments: %w", err)
		}
	}
	return nil
}

func (s *Service) audit(ctx context.Context, tx *sql.Tx, req Request, action, target string, payload any) error {
	return s.auditOn(ctx, tx, req, action, "user", target, payload)
}

// auditOn writes the audit entry of a successful change inside tx.
func (s *Service) auditOn(ctx context.Context, tx *sql.Tx, req Request, action, targetType, target string, payload any) error {
	_, err := audit.Write(ctx, tx, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		TargetType: targetType, TargetID: target, Action: action, Payload: payload, Result: audit.ResultOK,
	})
	return err
}

// rightsChanged drops cached rights and ends the sessions of the affected
// users, because their rights changed (AUTH-006, decision W2).
func (s *Service) rightsChanged(ctx context.Context, req Request, cause string, userIDs ...string) error {
	s.perm.Invalidate()
	if s.revoke == nil {
		return nil
	}
	for _, id := range userIDs {
		if err := s.revoke(ctx, id, cause, req.RequestID); err != nil {
			return err
		}
	}
	return nil
}
