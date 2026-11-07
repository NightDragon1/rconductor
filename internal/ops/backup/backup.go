// SPDX-License-Identifier: AGPL-3.0-or-later

// Package backup writes consistent copies of the panel database to the
// backup directory (NFR-008). The master key lives in /secrets and is never
// part of a backup (SEC-005).
package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/store"
)

// Kind tells why a backup was made; each kind has its own retention.
type Kind string

// Backup kinds.
const (
	KindAuto       Kind = "auto"
	KindPreMigrate Kind = "pre-migrate"
)

// Settings keys and their defaults (decision for M0, stored in settings).
const (
	SettingInterval         = "backup.interval"
	SettingKeepAuto         = "backup.keep_auto"
	SettingKeepPreMigration = "backup.keep_pre_migration"

	DefaultInterval         = 24 * time.Hour
	DefaultKeepAuto         = 7
	DefaultKeepPreMigration = 3

	// MinInterval guards against a misconfigured interval filling the disk.
	MinInterval = time.Hour
)

const tsLayout = "20060102T150405.000Z"

var nameRE = regexp.MustCompile(`^rconductor-(\d{8}T\d{6}\.\d{3}Z)-(auto|pre-migrate)(-v\d+)?\.db$`)

// Policy holds the backup settings in effect.
type Policy struct {
	Interval         time.Duration
	KeepAuto         int
	KeepPreMigration int
}

// DefaultPolicy returns the defaults.
func DefaultPolicy() Policy {
	return Policy{Interval: DefaultInterval, KeepAuto: DefaultKeepAuto, KeepPreMigration: DefaultKeepPreMigration}
}

func (p Policy) keep(k Kind) int {
	if k == KindPreMigrate {
		return p.KeepPreMigration
	}
	return p.KeepAuto
}

// EnsureDefaults stores the default backup settings unless they exist.
func EnsureDefaults(ctx context.Context, s *store.Store) error {
	for key, v := range map[string]any{
		SettingInterval:         DefaultInterval.String(),
		SettingKeepAuto:         DefaultKeepAuto,
		SettingKeepPreMigration: DefaultKeepPreMigration,
	} {
		if _, err := s.EnsureDefault(ctx, key, v); err != nil {
			return err
		}
	}
	return nil
}

// LoadPolicy reads the backup settings. Missing or invalid values fall back
// to the defaults with a warning, so that a bad setting never stops backups.
func LoadPolicy(ctx context.Context, s *store.Store, log *slog.Logger) Policy {
	p := DefaultPolicy()
	var interval string
	if err := s.GlobalSetting(ctx, SettingInterval, &interval); err == nil {
		d, err := time.ParseDuration(interval)
		switch {
		case err != nil:
			log.Warn("invalid backup interval, using default", slog.String("value", interval))
		case d < MinInterval:
			log.Warn("backup interval below minimum, using minimum", slog.String("value", interval))
			p.Interval = MinInterval
		default:
			p.Interval = d
		}
	}
	for key, dst := range map[string]*int{SettingKeepAuto: &p.KeepAuto, SettingKeepPreMigration: &p.KeepPreMigration} {
		var n int
		if err := s.GlobalSetting(ctx, key, &n); err == nil {
			if n < 1 {
				log.Warn("backup retention below 1, using 1", slog.String("setting", key))
				n = 1
			}
			*dst = n
		}
	}
	return p
}

// Backuper writes backups of one database into one directory.
type Backuper struct {
	DB  *sql.DB
	Dir string
	Log *slog.Logger
	now func() time.Time
}

// New returns a Backuper.
func New(db *sql.DB, dir string, log *slog.Logger) *Backuper {
	return &Backuper{DB: db, Dir: dir, Log: log, now: time.Now}
}

// Run writes one backup of the given kind and prunes old backups of that
// kind down to the policy. fromVersion is only used in pre-migration names.
// It logs backup.done or backup.failed.
func (b *Backuper) Run(ctx context.Context, kind Kind, fromVersion int64, p Policy) (string, error) {
	start := b.now()
	path, err := b.write(ctx, kind, fromVersion, start)
	if err != nil {
		b.Log.Error("backup failed", logging.Event(logging.BackupFailed),
			slog.String("kind", string(kind)), slog.Any("err", err))
		return "", err
	}
	pruned, err := b.prune(kind, p.keep(kind))
	if err != nil {
		b.Log.Warn("pruning old backups failed", slog.String("kind", string(kind)), slog.Any("err", err))
	}
	var size int64
	if fi, err := os.Stat(path); err == nil {
		size = fi.Size()
	}
	b.Log.Info("backup written", logging.Event(logging.BackupDone),
		slog.String("file", filepath.Base(path)), slog.String("kind", string(kind)),
		slog.Int64("bytes", size), slog.Int("pruned", pruned), logging.Duration(time.Since(start)))
	return path, nil
}

func (b *Backuper) write(ctx context.Context, kind Kind, fromVersion int64, at time.Time) (string, error) {
	if err := os.MkdirAll(b.Dir, 0o750); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}
	name := "rconductor-" + at.UTC().Format(tsLayout) + "-" + string(kind)
	if kind == KindPreMigrate {
		name += fmt.Sprintf("-v%d", fromVersion)
	}
	final := filepath.Join(b.Dir, name+".db")
	partial := final + ".partial"
	_ = os.Remove(partial)

	// VACUUM INTO writes a consistent, compacted snapshot without blocking
	// readers; the rename makes the backup appear only when complete.
	if _, err := b.DB.ExecContext(ctx, "VACUUM INTO ?", partial); err != nil {
		_ = os.Remove(partial)
		return "", fmt.Errorf("write snapshot: %w", err)
	}
	if err := os.Chmod(partial, 0o600); err != nil {
		_ = os.Remove(partial)
		return "", fmt.Errorf("set permissions: %w", err)
	}
	if err := os.Rename(partial, final); err != nil {
		_ = os.Remove(partial)
		return "", fmt.Errorf("finish backup: %w", err)
	}
	return final, nil
}

// Entry is one backup file.
type Entry struct {
	Name string
	Kind Kind
	At   time.Time
}

// List returns the backups in dir, oldest first. Unknown files are ignored.
func List(dir string) ([]Entry, error) {
	des, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, de := range des {
		m := nameRE.FindStringSubmatch(de.Name())
		if m == nil || !de.Type().IsRegular() {
			continue
		}
		at, err := time.Parse(tsLayout, m[1])
		if err != nil {
			continue
		}
		out = append(out, Entry{Name: de.Name(), Kind: Kind(m[2]), At: at})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}

func (b *Backuper) prune(kind Kind, keep int) (int, error) {
	all, err := List(b.Dir)
	if err != nil {
		return 0, err
	}
	var same []Entry
	for _, e := range all {
		if e.Kind == kind {
			same = append(same, e)
		}
	}
	removed := 0
	var errs []error
	for i := 0; i < len(same)-keep; i++ {
		if err := os.Remove(filepath.Join(b.Dir, same[i].Name)); err != nil {
			errs = append(errs, err)
			continue
		}
		removed++
	}
	return removed, errors.Join(errs...)
}

// LastAuto returns the time of the newest automatic backup, or zero.
func LastAuto(dir string) time.Time {
	all, err := List(dir)
	if err != nil {
		return time.Time{}
	}
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Kind == KindAuto {
			return all[i].At
		}
	}
	return time.Time{}
}
