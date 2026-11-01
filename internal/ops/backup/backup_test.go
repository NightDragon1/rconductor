// SPDX-License-Identifier: AGPL-3.0-or-later

package backup

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
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
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/migrations"
)

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

func newLogger(w *syncBuffer) *slog.Logger {
	return logging.New(w, "panel", config.Log{Level: slog.LevelDebug, Format: "json"}).Module("backup")
}

func migratedStore(t *testing.T, dataDir string) *store.Store {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	var logs syncBuffer
	if _, err := s.Migrate(ctx, newLogger(&logs), store.MigrateOptions{}); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestBackupRestoresIntoEmptyInstallation covers NFR-008: a backup is a
// consistent database that a new installation can start from.
func TestBackupRestoresIntoEmptyInstallation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := migratedStore(t, filepath.Join(root, "data"))
	if err := EnsureDefaults(ctx, s); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureDefault(ctx, "test.marker", "before-backup"); err != nil {
		t.Fatal(err)
	}

	var logs syncBuffer
	b := New(s.DB, filepath.Join(root, "backups"), newLogger(&logs))
	path, err := b.Run(ctx, KindAuto, 0, DefaultPolicy())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(logs.String(), `"event":"backup.done"`) || !strings.Contains(logs.String(), filepath.Base(path)) {
		t.Errorf("backup.done with file name missing:\n%s", logs.String())
	}

	// Restore: copy the backup as the database of an empty installation.
	restored := filepath.Join(root, "restored")
	if err := os.MkdirAll(restored, 0o750); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(restored, store.FileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := store.Open(ctx, restored)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	res, err := r.Migrate(ctx, newLogger(&logs), store.MigrateOptions{})
	if err != nil || res.Applied != 0 {
		t.Fatalf("restored Migrate = %+v, %v", res, err)
	}
	var marker string
	if err := r.GlobalSetting(ctx, "test.marker", &marker); err != nil || marker != "before-backup" {
		t.Errorf("restored marker = %q, %v", marker, err)
	}
}

func TestPruneKeepsPerKind(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := migratedStore(t, filepath.Join(root, "data"))
	var logs syncBuffer
	b := New(s.DB, filepath.Join(root, "backups"), newLogger(&logs))

	clock := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	b.now = func() time.Time { return clock }
	p := DefaultPolicy()
	for i := 0; i < 9; i++ {
		if _, err := b.Run(ctx, KindAuto, 0, p); err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(time.Hour)
	}
	for i := 0; i < 5; i++ {
		if _, err := b.Run(ctx, KindPreMigrate, int64(i+1), p); err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(time.Hour)
	}

	all, err := List(b.Dir)
	if err != nil {
		t.Fatal(err)
	}
	count := map[Kind]int{}
	for _, e := range all {
		count[e.Kind]++
	}
	if count[KindAuto] != DefaultKeepAuto || count[KindPreMigrate] != DefaultKeepPreMigration {
		t.Errorf("kept %v, want %d auto and %d pre-migrate", count, DefaultKeepAuto, DefaultKeepPreMigration)
	}
	// The oldest auto backups are the ones removed.
	if first := all[0]; first.Kind != KindAuto || !first.At.Equal(time.Date(2026, 11, 1, 5, 0, 0, 0, time.UTC)) {
		t.Errorf("oldest kept backup = %+v", first)
	}
	matches, _ := filepath.Glob(filepath.Join(b.Dir, "*.partial"))
	if len(matches) != 0 {
		t.Errorf("partial files left: %v", matches)
	}
}

// TestBackupBeforeMigration covers the backup part of NFR-013: migrating an
// existing database writes a pre-migration backup first; an empty first
// installation gets none.
func TestBackupBeforeMigration(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	backups := filepath.Join(root, "backups")
	var logs syncBuffer
	log := newLogger(&logs)

	s, err := store.Open(ctx, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	b := New(s.DB, backups, log)
	opts := store.MigrateOptions{BeforeMigrate: BeforeMigrate(s, b)}

	first, err := s.Migrate(ctx, log, opts)
	if err != nil {
		t.Fatal(err)
	}
	if all, _ := List(backups); len(all) != 0 {
		t.Fatalf("backup written for an empty first installation: %v", all)
	}

	// A newer release brings one more migration.
	next := fstest.MapFS{}
	if err := fs.WalkDir(migrations.SQLite(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(migrations.SQLite(), p)
		next[p] = &fstest.MapFile{Data: data}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	next["09999_next.sql"] = &fstest.MapFile{Data: []byte("-- +goose Up\nCREATE TABLE next_release (id TEXT PRIMARY KEY);\n-- +goose Down\nDROP TABLE next_release;\n")}
	opts.FS = next
	if _, err := s.Migrate(ctx, log, opts); err != nil {
		t.Fatal(err)
	}

	all, _ := List(backups)
	if want := fmt.Sprintf("-v%d.db", first.To); len(all) != 1 || all[0].Kind != KindPreMigrate || !strings.HasSuffix(all[0].Name, want) {
		t.Fatalf("backups = %+v, want one pre-migrate backup ending in %s", all, want)
	}
	out := logs.String()
	if i, j := strings.LastIndex(out, `"event":"backup.done"`), strings.LastIndex(out, `"event":"db.migrate.start"`); i < 0 || j < 0 || i > j {
		t.Errorf("backup.done must come before db.migrate.start:\n%s", out)
	}
}

func TestLoadPolicy(t *testing.T) {
	ctx := context.Background()
	s := migratedStore(t, t.TempDir())
	var logs syncBuffer
	log := newLogger(&logs)

	if p := LoadPolicy(ctx, s, log); p != DefaultPolicy() {
		t.Errorf("without settings = %+v, want defaults", p)
	}
	if err := EnsureDefaults(ctx, s); err != nil {
		t.Fatal(err)
	}
	if p := LoadPolicy(ctx, s, log); p != DefaultPolicy() {
		t.Errorf("with default settings = %+v", p)
	}

	set := func(key, value string) {
		if _, err := s.DB.ExecContext(ctx, `UPDATE settings SET value = ? WHERE key = ?`, value, key); err != nil {
			t.Fatal(err)
		}
	}
	set(SettingInterval, `"10m"`)
	set(SettingKeepAuto, `0`)
	set(SettingKeepPreMigration, `5`)
	p := LoadPolicy(ctx, s, log)
	if p.Interval != MinInterval || p.KeepAuto != 1 || p.KeepPreMigration != 5 {
		t.Errorf("clamped policy = %+v", p)
	}
	set(SettingInterval, `"often"`)
	if p := LoadPolicy(ctx, s, log); p.Interval != DefaultInterval {
		t.Errorf("invalid interval = %v, want default", p.Interval)
	}
}

func TestScheduleRunsWhenOverdue(t *testing.T) {
	root := t.TempDir()
	s := migratedStore(t, filepath.Join(root, "data"))
	var logs syncBuffer
	b := New(s.DB, filepath.Join(root, "backups"), newLogger(&logs))

	// An automatic backup from two days ago makes the next one overdue.
	if err := os.MkdirAll(b.Dir, 0o750); err != nil {
		t.Fatal(err)
	}
	old := "rconductor-" + time.Now().Add(-48*time.Hour).UTC().Format(tsLayout) + "-auto.db"
	if err := os.WriteFile(filepath.Join(b.Dir, old), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Schedule(ctx, s, b); close(done) }()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), `"event":"backup.done"`) {
		if time.Now().After(deadline) {
			t.Fatal("overdue backup not written")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
}
