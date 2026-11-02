// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/migrations"
)

// lockPollInterval is how often a waiting start retries the migration lock.
const lockPollInterval = 200 * time.Millisecond

// MigrateOptions configure Migrate.
type MigrateOptions struct {
	// FS holds the migrations; nil means the embedded SQLite set.
	FS fs.FS
	// BeforeMigrate runs after the lock is taken and before pending
	// migrations are applied to an existing database (from > 0). The
	// pre-migration backup hooks in here (NFR-013).
	BeforeMigrate func(ctx context.Context, from, to int64) error
}

// MigrateResult reports what Migrate did.
type MigrateResult struct {
	From, To int64
	Applied  int
}

// Migrate applies all pending migrations in one run, in order. Parallel
// starts against the same database wait for an exclusive file lock next to
// the database (NFR-013).
func (s *Store) Migrate(ctx context.Context, log *slog.Logger, opts MigrateOptions) (MigrateResult, error) {
	unlock, err := s.lockMigrations(ctx, log)
	if err != nil {
		return MigrateResult{}, err
	}
	defer unlock()
	return s.migrateLocked(ctx, log, opts)
}

func (s *Store) migrateLocked(ctx context.Context, log *slog.Logger, opts MigrateOptions) (MigrateResult, error) {
	fsys := opts.FS
	var popts []goose.ProviderOption
	if fsys == nil {
		fsys = migrations.SQLite()
		popts = append(popts, goose.WithGoMigrations(migrations.SQLiteGo()...))
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, s.DB, fsys, popts...)
	if err != nil {
		return MigrateResult{}, fmt.Errorf("load migrations: %w", err)
	}
	from, to, err := p.GetVersions(ctx)
	if err != nil {
		return MigrateResult{}, fmt.Errorf("read schema version: %w", err)
	}
	res := MigrateResult{From: from, To: from}
	if from >= to {
		log.Info("schema is up to date", logging.Event(logging.DBMigrateDone),
			slog.Int64("version", from), slog.Int("applied", 0))
		return res, nil
	}

	if from > 0 && opts.BeforeMigrate != nil {
		if err := opts.BeforeMigrate(ctx, from, to); err != nil {
			return res, fmt.Errorf("before migration: %w", err)
		}
	}

	start := time.Now()
	log.Info("applying migrations", logging.Event(logging.DBMigrateStart),
		slog.Int64("from", from), slog.Int64("to", to))
	results, err := p.Up(ctx)
	res.Applied = len(results)
	if len(results) > 0 {
		res.To = results[len(results)-1].Source.Version
	}
	if err != nil {
		return res, fmt.Errorf("apply migrations: %w", err)
	}
	log.Info("migrations applied", logging.Event(logging.DBMigrateDone),
		slog.Int64("from", from), slog.Int64("version", res.To), slog.Int("applied", res.Applied),
		logging.Duration(time.Since(start)))
	return res, nil
}

// lockMigrations takes the exclusive migration lock. If another process
// holds it, it logs db.migrate.lock_wait once and waits until the lock is
// free or ctx ends. The OS releases the lock if the holder crashes.
func (s *Store) lockMigrations(ctx context.Context, log *slog.Logger) (func(), error) {
	f, err := os.OpenFile(s.Path+".migrate.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open migration lock: %w", err)
	}
	unlock := func() {
		_ = unlockFile(f)
		_ = f.Close()
	}

	waited := false
	start := time.Now()
	for {
		err := tryLockFile(f)
		if err == nil {
			if waited {
				log.Debug("migration lock acquired", logging.Duration(time.Since(start)))
			}
			return unlock, nil
		}
		if !errors.Is(err, errLocked) {
			_ = f.Close()
			return nil, fmt.Errorf("take migration lock: %w", err)
		}
		if !waited {
			waited = true
			log.Info("waiting for migration lock held by another start",
				logging.Event(logging.DBMigrateLockWt))
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(lockPollInterval):
		}
	}
}
