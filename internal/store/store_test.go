// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
)

// syncBuffer is a log sink that is safe for concurrent writers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func testLogger(w *syncBuffer) *slog.Logger {
	return logging.New(w, "panel", config.Log{Level: slog.LevelDebug, Format: "json"}).Module("db")
}

func openStore(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// testMigrations returns the first n of three test migrations.
func testMigrations(n int) fstest.MapFS {
	all := []string{
		"00001_a.sql", "-- +goose Up\nCREATE TABLE a (id TEXT PRIMARY KEY);\n-- +goose Down\nDROP TABLE a;\n",
		"00002_b.sql", "-- +goose Up\nCREATE TABLE b (id TEXT PRIMARY KEY);\n-- +goose Down\nDROP TABLE b;\n",
		"00003_c.sql", "-- +goose Up\nALTER TABLE a ADD COLUMN name TEXT;\n-- +goose Down\nALTER TABLE a DROP COLUMN name;\n",
	}
	fsys := fstest.MapFS{}
	for i := 0; i < n; i++ {
		fsys[all[2*i]] = &fstest.MapFile{Data: []byte(all[2*i+1])}
	}
	return fsys
}

// TestFreshStart covers NFR-002: an empty data directory gets
// rconductor.db with all embedded migrations applied.
func TestFreshStart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	var logs syncBuffer
	s := openStore(t, dir)

	called := false
	res, err := s.Migrate(context.Background(), testLogger(&logs), MigrateOptions{
		BeforeMigrate: func(context.Context, int64, int64) error { called = true; return nil },
	})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Fatalf("database file missing: %v", err)
	}
	if res.From != 0 || res.To < 1 || res.Applied < 1 {
		t.Errorf("result = %+v", res)
	}
	if called {
		t.Error("BeforeMigrate ran for an empty first installation")
	}
	for _, code := range []logging.Code{logging.DBMigrateStart, logging.DBMigrateDone} {
		if !strings.Contains(logs.String(), `"event":"`+string(code)+`"`) {
			t.Errorf("missing event %s in logs:\n%s", code, logs.String())
		}
	}
	if _, err := s.DB.ExecContext(context.Background(), `SELECT id, key, value, scope_type, scope_id, updated_at FROM settings`); err != nil {
		t.Errorf("settings table not usable: %v", err)
	}
}

// TestMigrateSkipsVersions covers NFR-013: a database two versions behind
// is brought up to date in one run, with the pre-migration hook first.
func TestMigrateSkipsVersions(t *testing.T) {
	dir := t.TempDir()
	var logs syncBuffer
	s := openStore(t, dir)
	ctx := context.Background()

	if _, err := s.Migrate(ctx, testLogger(&logs), MigrateOptions{FS: testMigrations(1)}); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}

	var hookFrom, hookTo int64
	res, err := s.Migrate(ctx, testLogger(&logs), MigrateOptions{
		FS: testMigrations(3),
		BeforeMigrate: func(_ context.Context, from, to int64) error {
			hookFrom, hookTo = from, to
			return nil
		},
	})
	if err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if res.From != 1 || res.To != 3 || res.Applied != 2 {
		t.Errorf("result = %+v, want from 1 to 3 with 2 applied", res)
	}
	if hookFrom != 1 || hookTo != 3 {
		t.Errorf("BeforeMigrate(%d, %d), want (1, 3)", hookFrom, hookTo)
	}
	if _, err := s.DB.ExecContext(ctx, `SELECT name FROM a`); err != nil {
		t.Errorf("migration 3 not applied: %v", err)
	}

	// Up to date: nothing applied, no hook.
	res, err = s.Migrate(ctx, testLogger(&logs), MigrateOptions{
		FS:            testMigrations(3),
		BeforeMigrate: func(context.Context, int64, int64) error { t.Error("hook ran without pending migrations"); return nil },
	})
	if err != nil || res.Applied != 0 {
		t.Errorf("third Migrate = %+v, %v", res, err)
	}
}

// TestParallelStartWaits covers NFR-013: a second start waits for the lock
// of the first and then finds the schema up to date.
func TestParallelStartWaits(t *testing.T) {
	dir := t.TempDir()
	first := openStore(t, dir)
	second := openStore(t, dir)
	var logs syncBuffer
	log := testLogger(&logs)
	ctx := context.Background()

	unlock, err := first.lockMigrations(ctx, log)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}

	done := make(chan MigrateResult, 1)
	errs := make(chan error, 1)
	go func() {
		res, err := second.Migrate(ctx, log, MigrateOptions{FS: testMigrations(2)})
		errs <- err
		done <- res
	}()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), string(logging.DBMigrateLockWt)) {
		if time.Now().After(deadline) {
			t.Fatal("second start did not report db.migrate.lock_wait")
		}
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case err := <-errs:
		t.Fatalf("second start finished while the lock was held: %v", err)
	default:
	}

	// The first start migrates while holding the lock, then releases it.
	if _, err := first.migrateLocked(ctx, log, MigrateOptions{FS: testMigrations(2)}); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	unlock()

	if err := <-errs; err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if res := <-done; res.To != 2 {
		t.Errorf("second Migrate result = %+v", res)
	}
}

func TestMigrateLockHonorsContext(t *testing.T) {
	dir := t.TempDir()
	s := openStore(t, dir)
	var logs syncBuffer
	unlock, err := s.lockMigrations(context.Background(), testLogger(&logs))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := s.Migrate(ctx, testLogger(&logs), MigrateOptions{FS: testMigrations(1)}); err == nil {
		t.Fatal("Migrate succeeded while lock was held")
	}
}

func TestSettingsDefaults(t *testing.T) {
	s := openStore(t, t.TempDir())
	var logs syncBuffer
	ctx := context.Background()
	if _, err := s.Migrate(ctx, testLogger(&logs), MigrateOptions{}); err != nil {
		t.Fatal(err)
	}

	wrote, err := s.EnsureDefault(ctx, "test.interval", "24h")
	if err != nil || !wrote {
		t.Fatalf("EnsureDefault = %v, %v", wrote, err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE settings SET value = '"12h"' WHERE key = 'test.interval'`); err != nil {
		t.Fatal(err)
	}
	wrote, err = s.EnsureDefault(ctx, "test.interval", "24h")
	if err != nil || wrote {
		t.Fatalf("second EnsureDefault = %v, %v; must not overwrite", wrote, err)
	}
	var got string
	if err := s.GlobalSetting(ctx, "test.interval", &got); err != nil || got != "12h" {
		t.Errorf("GlobalSetting = %q, %v; want 12h", got, err)
	}

	var id string
	if err := s.DB.QueryRowContext(ctx, `SELECT id FROM settings WHERE key = 'test.interval'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if len(id) != 36 || id[14] != '7' {
		t.Errorf("id %q is not a UUIDv7", id)
	}
}
