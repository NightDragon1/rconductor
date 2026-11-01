// SPDX-License-Identifier: AGPL-3.0-or-later

// Package auth is the authentication module of the panel: emergency admin,
// login, sessions and login protection (AUTH-*, SEC-004).
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/auth/password"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// EmergencyPseudonym is the login name of the emergency admin (AUTH-003).
const EmergencyPseudonym = "admin"

// Role names as stored in the database (ROLE-001).
const (
	RoleRCONAdmin   = "rcon_admin"
	RoleServerAdmin = "server_admin"
	RoleModerator   = "moderator"
	RoleSupporter   = "supporter"
)

// initialPasswordLen gives about 119 bits from the alphabet below.
const initialPasswordLen = 20

const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

// EnsureEmergencyAdmin creates the emergency admin on the first start with
// the flag enabled (AUTH-003, AUTH-007). The random initial password is
// written exactly once to console, outside the event log (Design 8.1);
// the log gets auth.initial_password.issued without the password. Later
// starts find the admin and write nothing. With the flag disabled no
// admin is created; an existing one stays but cannot log in.
func EnsureEmergencyAdmin(ctx context.Context, st *store.Store, h *password.Hasher, enabled bool,
	log *slog.Logger, console io.Writer,
) error {
	if !enabled {
		return nil
	}
	_, err := st.Queries.GetEmergencyAdmin(ctx)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("look up emergency admin: %w", err)
	}

	pw, err := randomPassword(initialPasswordLen)
	if err != nil {
		return err
	}
	hash, err := h.Hash(ctx, pw)
	if err != nil {
		return fmt.Errorf("hash initial password: %w", err)
	}
	id, err := store.NewID()
	if err != nil {
		return err
	}

	tx, err := st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := store.Now()
	if err := sqlitedb.New(tx).InsertUser(ctx, sqlitedb.InsertUserParams{
		ID: id, Pseudonym: EmergencyPseudonym, Role: RoleRCONAdmin, PasswordHash: hash,
		IsEmergency: 1, MustChangePassword: 1, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		return fmt.Errorf("create emergency admin (is the pseudonym %q taken?): %w", EmergencyPseudonym, err)
	}
	if _, err := audit.Write(ctx, tx, audit.Entry{
		Kind: audit.KindAuth, TargetType: "user", TargetID: id,
		Action: "emergency_admin.create", Result: audit.ResultOK,
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("create emergency admin: %w", err)
	}

	log.Info("initial password of the emergency admin issued",
		logging.Event(logging.AuthInitialPassword), slog.String(logging.KeyUser, EmergencyPseudonym))
	_, err = fmt.Fprintf(console, "\n"+
		"=====================================================================\n"+
		" RCONductor: initial password of the emergency admin\n"+
		"   user:     %s\n"+
		"   password: %s\n"+
		" Shown only once. Log in and change it under your profile.\n"+
		"=====================================================================\n\n",
		EmergencyPseudonym, pw)
	return err
}

// RandomPassword returns a random password of n characters from an
// alphabet without look-alike characters.
func RandomPassword(n int) (string, error) { return randomPassword(n) }

func randomPassword(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// Rejection sampling keeps the characters uniformly distributed.
	out := make([]byte, 0, n)
	limit := 256 - 256%len(alphabet)
	for len(out) < n {
		for _, c := range b {
			if int(c) < limit && len(out) < n {
				out = append(out, alphabet[int(c)%len(alphabet)])
			}
		}
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
	}
	return string(out), nil
}
