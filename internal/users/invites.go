// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/auth"
	"github.com/NightDragon1/rconductor/internal/auth/password"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// Settings key of the invite lifetime and its default (decision M1).
const (
	SettingInviteLifetime = "invite.lifetime"
	DefaultInviteLifetime = 72 * time.Hour
)

// Pseudonym rules (AUTH-009): required and unique; letters, digits, space
// and . _ - only, so names render safely everywhere.
const (
	minPseudonym = 2
	maxPseudonym = 32
)

// ErrInviteInvalid answers every unusable token alike: unknown, expired or
// already used.
var ErrInviteInvalid = errors.New("the invitation is invalid or has expired")

// Invite is an invitation as shown to its creator.
type Invite struct {
	ID             string
	CreatedBy      string
	Role           perm.Role
	TemplateUserID string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	UsedAt         *time.Time
	UsedBy         string
}

// Invites creates and redeems invitations.
type Invites struct {
	users  *Service
	hasher *password.Hasher
	now    func() time.Time
}

// NewInvites returns the invitation service.
func NewInvites(users *Service, h *password.Hasher) *Invites {
	return &Invites{users: users, hasher: h, now: time.Now}
}

// EnsureDefaults stores the default user settings unless they exist.
func EnsureDefaults(ctx context.Context, st *store.Store) error {
	_, err := st.EnsureDefault(ctx, SettingInviteLifetime, DefaultInviteLifetime.String())
	return err
}

func (iv *Invites) lifetime(ctx context.Context) time.Duration {
	var v string
	if err := iv.users.st.GlobalSetting(ctx, SettingInviteLifetime, &v); err == nil {
		if d, err := time.ParseDuration(v); err == nil && d >= time.Minute {
			return d
		}
	}
	return DefaultInviteLifetime
}

// ValidPseudonym checks the pseudonym rules.
func ValidPseudonym(p string) (string, error) {
	p = strings.TrimSpace(p)
	n := utf8.RuneCountInString(p)
	if n < minPseudonym || n > maxPseudonym {
		return "", &InvalidError{Detail: fmt.Sprintf("a pseudonym has %d to %d characters", minPseudonym, maxPseudonym)}
	}
	for _, r := range p {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(" ._-", r) {
			return "", &InvalidError{Detail: "a pseudonym has only letters, digits, space and . _ -"}
		}
	}
	if strings.EqualFold(p, auth.EmergencyPseudonym) {
		return "", &InvalidError{Detail: "this pseudonym is reserved"}
	}
	return p, nil
}

// mayInvite: a role strictly below the caller's; a moderator invites
// supporters only while supporters are enabled for him (ROLE-006).
func (iv *Invites) mayInvite(ctx context.Context, req Request, role perm.Role) (bool, error) {
	if !role.Valid() || !req.Actor.Role.Above(role) {
		return false, nil
	}
	return iv.users.perm.MayManageUsers(ctx, req.Actor)
}

// Create makes an invitation and returns it with its token, which is
// shown only now.
func (iv *Invites) Create(ctx context.Context, req Request, role perm.Role, templateID string) (Invite, string, error) {
	s := iv.users
	ok, err := iv.mayInvite(ctx, req, role)
	if err != nil {
		return Invite{}, "", err
	}
	if !ok {
		s.deniedOn(ctx, req, "invite.create", "invite", "", "forbidden", map[string]string{"role": string(role)})
		return Invite{}, "", ErrForbidden
	}
	if templateID != "" {
		t, err := s.Get(ctx, req, templateID)
		if err != nil {
			return Invite{}, "", err
		}
		if t.Role != role {
			return Invite{}, "", &InvalidError{Detail: "the template user must have the invited role"}
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Invite{}, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	id, err := store.NewID()
	if err != nil {
		return Invite{}, "", err
	}
	now := iv.now().UTC()
	expires := now.Add(iv.lifetime(ctx))
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return Invite{}, "", err
	}
	defer func() { _ = tx.Rollback() }()
	if err := sqlitedb.New(tx).InsertInvite(ctx, sqlitedb.InsertInviteParams{
		ID: id, TokenHash: auth.HashToken(token), CreatedBy: req.Actor.ID, Role: string(role),
		TemplateUserID: sql.NullString{String: templateID, Valid: templateID != ""},
		CreatedAt:      now.Format(store.TimeLayout), ExpiresAt: expires.Format(store.TimeLayout),
	}); err != nil {
		return Invite{}, "", err
	}
	if err := s.auditOn(ctx, tx, req, "invite.create", "invite", id, map[string]string{
		"role": string(role), "template_user_id": templateID, "expires_at": expires.Format(time.RFC3339),
	}); err != nil {
		return Invite{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return Invite{}, "", err
	}
	return Invite{ID: id, CreatedBy: req.Actor.ID, Role: role, TemplateUserID: templateID, CreatedAt: now, ExpiresAt: expires}, token, nil
}

func toInvite(r sqlitedb.ListInvitesRow) Invite {
	inv := Invite{ID: r.ID, CreatedBy: r.CreatedBy, Role: perm.Role(r.Role), TemplateUserID: r.TemplateUserID.String, UsedBy: r.UsedBy.String}
	inv.CreatedAt, _ = time.Parse(store.TimeLayout, r.CreatedAt)
	inv.ExpiresAt, _ = time.Parse(store.TimeLayout, r.ExpiresAt)
	if r.UsedAt.Valid {
		if t, err := time.Parse(store.TimeLayout, r.UsedAt.String); err == nil {
			inv.UsedAt = &t
		}
	}
	return inv
}

// List returns the invitations of the caller; the RCON admin sees all.
func (iv *Invites) List(ctx context.Context, req Request) ([]Invite, error) {
	if err := iv.users.mayManageUsers(ctx, req, "invite.list"); err != nil {
		return nil, err
	}
	rows, err := iv.users.st.Queries.ListInvites(ctx)
	if err != nil {
		return nil, err
	}
	out := []Invite{}
	for _, r := range rows {
		if req.Actor.Role == perm.RCONAdmin || r.CreatedBy == req.Actor.ID {
			out = append(out, toInvite(r))
		}
	}
	return out, nil
}

// Delete withdraws an unused invitation of the caller (any for the RCON
// admin).
func (iv *Invites) Delete(ctx context.Context, req Request, id string) error {
	s := iv.users
	if err := s.mayManageUsers(ctx, req, "invite.delete"); err != nil {
		return err
	}
	r, err := s.st.Queries.GetInvite(ctx, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && req.Actor.Role != perm.RCONAdmin && r.CreatedBy != req.Actor.ID) {
		s.deniedOn(ctx, req, "invite.delete", "invite", id, "not_found", nil)
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if r.UsedAt.Valid {
		return &InvalidError{Detail: "the invitation has been used"}
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := sqlitedb.New(tx).DeleteInvite(ctx, id); err != nil {
		return err
	}
	if err := s.auditOn(ctx, tx, req, "invite.delete", "invite", id, nil); err != nil {
		return err
	}
	return tx.Commit()
}

// Redeem creates the invited user with his own pseudonym and password. The
// token is checked before any password work, so invalid tokens cost
// nothing. The user takes the template's assignments as far as the
// inviter controls them.
func (iv *Invites) Redeem(ctx context.Context, token, pseudonym, pw, requestID string) (User, error) {
	s := iv.users
	inv, err := s.st.Queries.GetInviteByToken(ctx, auth.HashToken(token))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrInviteInvalid
	}
	if err != nil {
		return User{}, err
	}
	expires, err := time.Parse(store.TimeLayout, inv.ExpiresAt)
	if err != nil || inv.UsedAt.Valid || !iv.now().Before(expires) {
		return User{}, ErrInviteInvalid
	}
	if pseudonym, err = ValidPseudonym(pseudonym); err != nil {
		return User{}, err
	}
	if err := password.Validate(pw); err != nil {
		return User{}, &InvalidError{Detail: err.Error()}
	}
	hash, err := iv.hasher.Hash(ctx, pw)
	if err != nil {
		return User{}, err
	}
	id, err := store.NewID()
	if err != nil {
		return User{}, err
	}

	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	now := store.Now()
	if err := q.InsertUser(ctx, sqlitedb.InsertUserParams{
		ID: id, Pseudonym: pseudonym, Role: inv.Role, PasswordHash: hash,
		CreatedBy: sql.NullString{String: inv.CreatedBy, Valid: true}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return User{}, &InvalidError{Detail: "this pseudonym is taken"}
		}
		return User{}, err
	}
	n, err := q.UseInvite(ctx, sqlitedb.UseInviteParams{
		UsedAt: sql.NullString{String: now, Valid: true}, UsedBy: sql.NullString{String: id, Valid: true}, ID: inv.ID,
	})
	if err != nil {
		return User{}, err
	}
	if n != 1 {
		return User{}, ErrInviteInvalid // redeemed concurrently
	}
	if _, err := audit.Write(ctx, tx, audit.Entry{
		Kind: audit.KindAuth, ActorID: id, ActorRole: inv.Role, TargetType: "invite", TargetID: inv.ID,
		Action: "invite.redeem", Result: audit.ResultOK, Payload: map[string]string{"request_id": requestID},
	}); err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	s.log.Info("invitation used", logging.Event(logging.AuthInviteUsed), slog.String(logging.KeyRequestID, requestID),
		slog.String(logging.KeyUser, pseudonym), slog.String(logging.KeyRole, inv.Role))

	if inv.TemplateUserID.Valid {
		iv.copyTemplate(ctx, inv.CreatedBy, inv.TemplateUserID.String, id, requestID)
	}
	return s.load(ctx, id)
}

// copyTemplate gives the new user the template's assignments in the name
// of the inviter, with the same checks as a manual assignment. A failure
// leaves the user without them and is logged.
func (iv *Invites) copyTemplate(ctx context.Context, inviter, template, user, requestID string) {
	s := iv.users
	r, err := s.perm.For(ctx, inviter)
	if err != nil {
		s.log.Error("template not applied", slog.Any("err", err))
		return
	}
	req := Request{Actor: perm.Actor{ID: inviter, Role: r.Role}, RequestID: requestID}
	a, err := s.assignments(ctx, template)
	if err == nil {
		groups := []string{}
		for _, g := range a.Groups {
			if gr, err := s.st.Queries.GetActiveGroup(ctx, g); err == nil && (req.Actor.Role == perm.RCONAdmin || gr.OwnerID == inviter) {
				groups = append(groups, g)
			}
		}
		up := AssignmentsUpdate{Groups: &groups}
		if r.Role == perm.RCONAdmin && a.Role == perm.ServerAdmin {
			up.AdminScopes = &a.AdminScopes
		}
		if a.Role == perm.Moderator {
			grants := []Grant{}
			for _, g := range a.Grants {
				if slices.Contains(groups, g.GroupID) {
					grants = append(grants, g)
				}
			}
			up.Grants = &grants
		}
		if a.Role == perm.Supporter {
			sg := []SupporterGrant{}
			for _, g := range a.SupporterGrants {
				if g.ModeratorID == inviter {
					sg = append(sg, g)
				}
			}
			up.SupporterGrants = &sg
		}
		_, err = s.UpdateAssignments(ctx, req, user, up)
	}
	if err != nil {
		s.log.Error("template not applied", slog.String("user_id", user), slog.Any("err", err))
	}
}
