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

// EmergencyOptions are RCD_EMERGENCY_ADMIN and RCD_EMERGENCY_ADMIN_RESET.
type EmergencyOptions struct {
	Enabled bool
	Reset   bool
}

// EnsureEmergencyAdmin creates the emergency admin on the first start with
// the flag enabled (AUTH-003, AUTH-007). The random initial password is
// written exactly once to console, outside the event log (Design 8.1);
// the log gets auth.initial_password.issued without the password. Later
// starts find the admin and write nothing, unless Reset asks for a new
// initial password (AUTH-011). With the flag disabled no admin is created;
// an existing one stays but cannot log in.
func EnsureEmergencyAdmin(ctx context.Context, st *store.Store, h *password.Hasher, opts EmergencyOptions,
	log *slog.Logger, console io.Writer,
) error {
	if !opts.Enabled {
		if opts.Reset {
			log.Warn("RCD_EMERGENCY_ADMIN_RESET is ignored while RCD_EMERGENCY_ADMIN=false")
		}
		return nil
	}
	admin, err := st.Queries.GetEmergencyAdmin(ctx)
	switch {
	case err == nil && opts.Reset:
		return resetEmergencyAdmin(ctx, st, h, admin.ID, log, console)
	case err == nil:
		return nil
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("look up emergency admin: %w", err)
	}

	pw, hash, err := newInitialPassword(ctx, h)
	if err != nil {
		return err
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
	return printInitialPassword(console, pw)
}

// resetEmergencyAdmin gives the emergency admin a new initial password
// that must be changed, lifts a lock and ends all his sessions (AUTH-011).
func resetEmergencyAdmin(ctx context.Context, st *store.Store, h *password.Hasher, id string,
	log *slog.Logger, console io.Writer,
) error {
	pw, hash, err := newInitialPassword(ctx, h)
	if err != nil {
		return err
	}
	tx, err := st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	if err := q.ResetEmergencyAdmin(ctx, sqlitedb.ResetEmergencyAdminParams{PasswordHash: hash, UpdatedAt: store.Now(), ID: id}); err != nil {
		return fmt.Errorf("reset emergency admin: %w", err)
	}
	sessions, err := q.DeleteUserSessions(ctx, id)
	if err != nil {
		return fmt.Errorf("end sessions of the emergency admin: %w", err)
	}
	if _, err := audit.Write(ctx, tx, audit.Entry{
		Kind: audit.KindAuth, TargetType: "user", TargetID: id,
		Action: "emergency_admin.reset", Result: audit.ResultOK,
		Payload: map[string]int{"sessions_ended": len(sessions)},
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("reset emergency admin: %w", err)
	}
	for _, sh := range sessions {
		log.Info("session ended", logging.Event(logging.AuthSessionRevoked),
			slog.String(logging.KeySession, sh[:8]), slog.String("user_id", id), slog.String("cause", "emergency_admin.reset"))
	}
	log.Warn("emergency admin reset with a new initial password; remove RCD_EMERGENCY_ADMIN_RESET, "+
		"otherwise every start resets it again",
		logging.Event(logging.AuthEmergencyReset), slog.String(logging.KeyUser, EmergencyPseudonym),
		slog.Int("sessions_ended", len(sessions)))
	log.Info("initial password of the emergency admin issued",
		logging.Event(logging.AuthInitialPassword), slog.String(logging.KeyUser, EmergencyPseudonym))
	return printInitialPassword(console, pw)
}

func newInitialPassword(ctx context.Context, h *password.Hasher) (pw, hash string, err error) {
	if pw, err = randomPassword(initialPasswordLen); err != nil {
		return "", "", err
	}
	if hash, err = h.Hash(ctx, pw); err != nil {
		return "", "", fmt.Errorf("hash initial password: %w", err)
	}
	return pw, hash, nil
}

func printInitialPassword(console io.Writer, pw string) error {
	_, err := fmt.Fprintf(console, "\n"+
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
