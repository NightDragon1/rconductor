// SPDX-License-Identifier: AGPL-3.0-or-later

// Package storetest provides a migrated temporary database and test
// records for tests of other packages.
package storetest

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/store"
)

// New returns a store in a temporary directory with all migrations applied.
func New(t testing.TB) *store.Store {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	log := logging.New(io.Discard, "panel", config.Log{}).Module("db")
	if _, err := st.Migrate(ctx, log, store.MigrateOptions{}); err != nil {
		t.Fatal(err)
	}
	return st
}

// User describes a test user; empty fields get defaults.
type User struct {
	Pseudonym    string
	Role         string // default moderator
	PasswordHash string // default "x", which matches no password
	Emergency    bool
	Disabled     bool
	CreatedBy    string
}

// CreateUser inserts u and returns its ID.
func CreateUser(t testing.TB, st *store.Store, u User) string {
	t.Helper()
	id, err := store.NewID()
	if err != nil {
		t.Fatal(err)
	}
	if u.Role == "" {
		u.Role = "moderator"
	}
	if u.PasswordHash == "" {
		u.PasswordHash = "x"
	}
	now := store.Now()
	var disabled, createdBy any
	if u.Disabled {
		disabled = now
	}
	if u.CreatedBy != "" {
		createdBy = u.CreatedBy
	}
	if _, err := st.DB.ExecContext(context.Background(),
		`INSERT INTO users (id, pseudonym, role, password_hash, is_emergency, created_by, created_at, updated_at, disabled_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, u.Pseudonym, u.Role, u.PasswordHash, u.Emergency, createdBy, now, now, disabled,
	); err != nil {
		t.Fatalf("create user %s: %v", u.Pseudonym, err)
	}
	return id
}
