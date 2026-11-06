// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/NightDragon1/rconductor/internal/auth"
	"github.com/NightDragon1/rconductor/internal/auth/password"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// Limits of the optional profile fields (AUTH-009).
const (
	maxRealName     = 64
	maxContacts     = 8
	maxContactValue = 128
)

var contactKey = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

// ErrWrongPassword means the current password did not match.
var ErrWrongPassword = errors.New("the current password is wrong")

// OtherSessionsRevoker ends all sessions of a user but one
// (auth.Service.RevokeOtherSessions).
type OtherSessionsRevoker func(ctx context.Context, userID, keepHash, reason, requestID string) error

// Profiles lets users maintain their own profile and password.
type Profiles struct {
	users        *Service
	hasher       *password.Hasher
	revokeOthers OtherSessionsRevoker
}

// NewProfiles returns the profile service.
func NewProfiles(users *Service, h *password.Hasher, revokeOthers OtherSessionsRevoker) *Profiles {
	return &Profiles{users: users, hasher: h, revokeOthers: revokeOthers}
}

// ProfileUpdate holds the changes of PATCH /me; nil fields stay. An empty
// real name removes it.
type ProfileUpdate struct {
	Pseudonym *string
	RealName  *string
	Contacts  *map[string]string
}

func plainText(s string, maxLen int, field string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > maxLen {
		return "", &InvalidError{Detail: fmt.Sprintf("%s has at most %d characters", field, maxLen)}
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", &InvalidError{Detail: field + " must not contain control characters"}
		}
	}
	return s, nil
}

// Me returns the caller's own profile.
func (p *Profiles) Me(ctx context.Context, req Request) (User, error) {
	return p.users.load(ctx, req.Actor.ID)
}

// Update changes the caller's own profile. The emergency admin keeps his
// pseudonym (AUTH-003).
func (p *Profiles) Update(ctx context.Context, req Request, up ProfileUpdate) (User, error) {
	s := p.users
	me, err := s.load(ctx, req.Actor.ID)
	if err != nil {
		return User{}, err
	}
	pseudonym, realName, contacts := me.Pseudonym, me.RealName, me.Contacts
	if up.Pseudonym != nil && *up.Pseudonym != me.Pseudonym {
		if me.IsEmergency {
			return User{}, &InvalidError{Detail: "the emergency admin keeps his pseudonym"}
		}
		if pseudonym, err = ValidPseudonym(*up.Pseudonym); err != nil {
			return User{}, err
		}
	}
	if up.RealName != nil {
		if realName, err = plainText(*up.RealName, maxRealName, "the real name"); err != nil {
			return User{}, err
		}
	}
	if up.Contacts != nil {
		if len(*up.Contacts) > maxContacts {
			return User{}, &InvalidError{Detail: fmt.Sprintf("at most %d contacts", maxContacts)}
		}
		contacts = map[string]string{}
		for k, v := range *up.Contacts {
			if !contactKey.MatchString(k) {
				return User{}, &InvalidError{Detail: "a contact name has 1 to 32 characters a-z, 0-9 and _"}
			}
			if contacts[k], err = plainText(v, maxContactValue, "a contact"); err != nil {
				return User{}, err
			}
		}
	}
	b, err := json.Marshal(contacts)
	if err != nil {
		return User{}, err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := sqlitedb.New(tx).UpdateProfile(ctx, sqlitedb.UpdateProfileParams{
		Pseudonym: pseudonym, RealName: sql.NullString{String: realName, Valid: realName != ""},
		Contacts: string(b), UpdatedAt: store.Now(), ID: me.ID,
	}); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return User{}, &InvalidError{Detail: "this pseudonym is taken"}
		}
		return User{}, err
	}
	// The audit names the changed fields, not their content.
	fields := []string{}
	for name, changed := range map[string]bool{"pseudonym": up.Pseudonym != nil, "real_name": up.RealName != nil, "contacts": up.Contacts != nil} {
		if changed {
			fields = append(fields, name)
		}
	}
	if err := s.audit(ctx, tx, req, "profile.update", me.ID, map[string]any{"fields": fields}); err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return s.load(ctx, me.ID)
}

// ChangePassword replaces the caller's password after checking the current
// one. The old password, including the initial password of the emergency
// admin, is invalid at once (AUTH-007), and all other sessions end.
func (p *Profiles) ChangePassword(ctx context.Context, req Request, current, next string) error {
	s := p.users
	hash, err := s.st.Queries.GetPasswordHash(ctx, req.Actor.ID)
	if err != nil {
		return fmt.Errorf("load password: %w", err)
	}
	ok, err := p.hasher.Verify(ctx, hash, current)
	if err != nil {
		return err
	}
	if !ok {
		s.denied(ctx, req, "password.change", req.Actor.ID, "unauthenticated", nil)
		return ErrWrongPassword
	}
	if err := password.Validate(next); err != nil {
		return &InvalidError{Detail: err.Error()}
	}
	// An initial or temporary password is known to someone else (AUTH-010).
	if next == current {
		return &InvalidError{Detail: "the new password must differ from the current one"}
	}
	newHash, err := p.hasher.Hash(ctx, next)
	if err != nil {
		return err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := sqlitedb.New(tx).SetPasswordHash(ctx, sqlitedb.SetPasswordHashParams{PasswordHash: newHash, UpdatedAt: store.Now(), ID: req.Actor.ID}); err != nil {
		return err
	}
	if err := s.audit(ctx, tx, req, "password.change", req.Actor.ID, nil); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if p.revokeOthers != nil {
		return p.revokeOthers(ctx, req.Actor.ID, req.SessionID, "password.change", req.RequestID)
	}
	return nil
}

// SetAvatar stores the caller's profile picture; nil data removes it.
func (p *Profiles) SetAvatar(ctx context.Context, req Request, data []byte) error {
	s := p.users
	var img []byte
	var typ sql.NullString
	if data != nil {
		out, mediaType, err := processAvatar(data)
		if err != nil {
			return err
		}
		img, typ = out, sql.NullString{String: mediaType, Valid: true}
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := sqlitedb.New(tx).SetAvatar(ctx, sqlitedb.SetAvatarParams{Avatar: img, AvatarType: typ, UpdatedAt: store.Now(), ID: req.Actor.ID}); err != nil {
		return err
	}
	action := "avatar.set"
	if data == nil {
		action = "avatar.remove"
	}
	if err := s.audit(ctx, tx, req, action, req.Actor.ID, nil); err != nil {
		return err
	}
	return tx.Commit()
}

// Avatar returns a profile picture for the user himself, the RCON admin
// and whoever manages him (decision W4); everyone else gets ErrNotFound.
func (p *Profiles) Avatar(ctx context.Context, req Request, id string) ([]byte, string, error) {
	s := p.users
	if id != req.Actor.ID && req.Actor.Role != perm.RCONAdmin {
		ok, err := s.perm.Manages(ctx, req.Actor, id)
		if err != nil {
			return nil, "", err
		}
		if !ok {
			return nil, "", ErrNotFound
		}
	}
	a, err := s.st.Queries.GetAvatar(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	return a.Avatar, a.AvatarType.String, nil
}

// tempPasswordLen gives about 119 bits, like the initial password.
const tempPasswordLen = 20

// ResetPassword sets a random temporary password for a managed user and
// returns it once for the managing user (AUTH-010). The user must change
// it at the next login, and all his sessions end. The password never goes
// to log or audit.
func (p *Profiles) ResetPassword(ctx context.Context, req Request, id string) (string, error) {
	s := p.users
	if err := s.mustManage(ctx, req, id, "password.reset"); err != nil {
		return "", err
	}
	u, err := s.load(ctx, id)
	if err != nil {
		return "", err
	}
	if u.IsEmergency {
		s.denied(ctx, req, "password.reset", id, "forbidden", nil)
		return "", ErrForbidden
	}
	pw, err := auth.RandomPassword(tempPasswordLen)
	if err != nil {
		return "", err
	}
	hash, err := p.hasher.Hash(ctx, pw)
	if err != nil {
		return "", err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	if err := sqlitedb.New(tx).ResetPassword(ctx, sqlitedb.ResetPasswordParams{PasswordHash: hash, UpdatedAt: store.Now(), ID: id}); err != nil {
		return "", err
	}
	if err := s.audit(ctx, tx, req, "password.reset", id, nil); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	if err := s.rightsChanged(ctx, req, "password.reset", id); err != nil {
		return "", err
	}
	return pw, nil
}
