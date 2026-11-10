// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// Settings key of the polling fallback and its default (revision 2.2
// candidate 2).
const (
	SettingPollInterval = "profiles.poll_interval"
	DefaultPollInterval = 30 * time.Second
	MinPollInterval     = time.Second
)

// debounce collects bursts of file events (editors write several times).
const debounce = 300 * time.Millisecond

// Importer brings the profile files into the database: at start and on
// every change, without restart (CAT-003). The UI reads profiles only from
// the database.
type Importer struct {
	st      *store.Store
	log     *slog.Logger
	builtin fs.FS
	dir     string // custom profiles, /data/profiles

	mu sync.Mutex // one import at a time

	// noNotify turns off file events, for tests of the polling fallback.
	noNotify bool
	// interval overrides the poll setting, for tests.
	interval time.Duration
}

// NewImporter returns an importer for the shipped profiles and the custom
// profile directory.
func NewImporter(st *store.Store, log *slog.Logger, builtin fs.FS, dir string) *Importer {
	return &Importer{st: st, log: log, builtin: builtin, dir: dir}
}

// EnsureDefaults stores the default profile settings unless they exist.
func EnsureDefaults(ctx context.Context, st *store.Store) error {
	_, err := st.EnsureDefault(ctx, SettingPollInterval, DefaultPollInterval.String())
	return err
}

type candidate struct {
	p          *Profile
	hash       string
	source     string
	overridden bool
}

// rulesFormat is part of the hash: a new format of the stored rules
// imports every profile once again (D-074: the fields a profile sets).
const rulesFormat = "rules-5\n"

func hashOf(b []byte) string {
	sum := sha256.Sum256(append([]byte(rulesFormat), b...))
	return hex.EncodeToString(sum[:])
}

func isProfileFile(name string) bool {
	return !strings.HasPrefix(name, ".") && (strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml"))
}

// Sync imports what changed. A file that is not valid is rejected with
// profile.rejected; the last valid state of its profile stays active and
// carries the error (Design 7.1).
func (im *Importer) Sync(ctx context.Context) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	final := map[string]candidate{}
	entries, err := fs.ReadDir(im.builtin, ".")
	if err != nil {
		return fmt.Errorf("read shipped profiles: %w", err)
	}
	for _, e := range entries {
		if !isProfileFile(e.Name()) {
			continue
		}
		data, err := fs.ReadFile(im.builtin, e.Name())
		if err != nil {
			return err
		}
		if c, ok := im.parse(ctx, data, "builtin:"+e.Name(), e.Name()); ok {
			final[c.p.ID] = c
		}
	}

	custom := map[string]bool{}
	for _, name := range im.customFiles() {
		data, err := readLimited(filepath.Join(im.dir, name))
		if err != nil {
			im.reject(ctx, name, strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml"), []string{err.Error()})
			continue
		}
		c, ok := im.parse(ctx, data, filepath.Join(im.dir, name), name)
		if !ok {
			continue
		}
		if custom[c.p.ID] {
			im.reject(ctx, name, c.p.ID, []string{fmt.Sprintf("another file in %s already has the id %s", im.dir, c.p.ID)})
			continue
		}
		custom[c.p.ID] = true
		_, shipped := final[c.p.ID]
		c.overridden = shipped
		final[c.p.ID] = c
	}

	slugs := make([]string, 0, len(final))
	for s := range final {
		slugs = append(slugs, s)
	}
	slices.Sort(slugs)
	for _, slug := range slugs {
		if err := im.store(ctx, final[slug]); err != nil {
			return err
		}
	}
	return nil
}

func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // a profile file: inside the profile directory, or named by the caller of profile check
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (im *Importer) customFiles() []string {
	entries, err := os.ReadDir(im.dir)
	if err != nil {
		return nil // no custom profiles
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && isProfileFile(e.Name()) {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	return names
}

func (im *Importer) parse(ctx context.Context, data []byte, source, name string) (candidate, bool) {
	p, err := Parse(data)
	if err != nil {
		var pe *Error
		probs := []string{err.Error()}
		if errors.As(err, &pe) {
			probs = pe.Problems
		}
		im.reject(ctx, name, strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml"), probs)
		return candidate{}, false
	}
	return candidate{p: p, hash: hashOf(data), source: source}, true
}

// reject logs profile.rejected and keeps the error at the profile of that
// id, if it exists; its last valid state stays.
func (im *Importer) reject(ctx context.Context, file, slug string, problems []string) {
	im.log.Warn("profile rejected", logging.Event(logging.ProfileRejected), slog.String(logging.KeyReason, "invalid"),
		slog.String("file", file), slog.String("profile", slug), slog.Any("problems", problems))
	if err := im.st.Queries.SetProfileError(ctx, sqlitedb.SetProfileErrorParams{
		LastError:   sql.NullString{String: strings.Join(problems, "; "), Valid: true},
		LastErrorAt: sql.NullString{String: store.Now(), Valid: true},
		Slug:        slug,
	}); err != nil {
		im.log.Error("profile error not stored", slog.Any("err", err))
	}
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// keepsProtocol refuses a new version of a profile that servers use when
// it changes the protocol or the http-api dialect: the profile list of a
// server never changes its protocol (D-076). The last valid state stays.
func (im *Importer) keepsProtocol(ctx context.Context, id string, c candidate) (bool, error) {
	old, err := im.st.Queries.GetGameProfile(ctx, id)
	if err != nil {
		return false, err
	}
	var rules Profile
	if err := json.Unmarshal([]byte(old.Rules), &rules); err != nil {
		return false, err
	}
	dialect := func(p *Profile) string {
		if p.HTTPAPI == nil {
			return ""
		}
		return p.HTTPAPI.Dialect
	}
	if old.Protocol == c.p.Protocol.Type && dialect(&rules) == dialect(c.p) {
		return true, nil
	}
	n, err := im.st.Queries.CountProfileServers(ctx, id)
	if err != nil || n == 0 {
		return err == nil, err
	}
	im.reject(ctx, filepath.Base(c.source), c.p.ID, []string{fmt.Sprintf(
		"protocol or http_api.dialect changed, but %d server(s) use the profile; the last valid state stays", n)})
	return false, nil
}

// store writes one profile and its catalog if its content or override
// state changed.
func (im *Importer) store(ctx context.Context, c candidate) error {
	cur, err := im.st.Queries.GetProfileBySlug(ctx, c.p.ID)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if exists && cur.ContentHash == c.hash && (cur.Overridden == 1) == c.overridden {
		return nil
	}
	if exists {
		if keep, err := im.keepsProtocol(ctx, cur.ID, c); err != nil || !keep {
			return err
		}
	}
	rules, err := json.Marshal(c.p)
	if err != nil {
		return err
	}
	tx, err := im.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	now := store.Now()
	id := cur.ID
	if exists {
		err = q.UpdateGameProfile(ctx, sqlitedb.UpdateGameProfileParams{
			Name: c.p.Name, Protocol: c.p.Protocol.Type, DefaultPort: int64(c.p.Protocol.DefaultPort), Rules: string(rules),
			SourceFile: c.source, ContentHash: c.hash, Overridden: boolInt(c.overridden), ImportedAt: now, ID: id,
		})
	} else {
		if id, err = store.NewID(); err != nil {
			return err
		}
		err = q.InsertGameProfile(ctx, sqlitedb.InsertGameProfileParams{
			ID: id, Slug: c.p.ID, Name: c.p.Name, Protocol: c.p.Protocol.Type, DefaultPort: int64(c.p.Protocol.DefaultPort),
			Rules: string(rules), SourceFile: c.source, ContentHash: c.hash, Overridden: boolInt(c.overridden), ImportedAt: now,
		})
	}
	if err != nil {
		return fmt.Errorf("store profile %s: %w", c.p.ID, err)
	}
	if err := q.DeleteCatalog(ctx, id); err != nil {
		return err
	}
	for i, cmd := range c.p.Catalog {
		cid, err := store.NewID()
		if err != nil {
			return err
		}
		params, err := json.Marshal(cmd.Params)
		if err != nil {
			return err
		}
		if err := q.InsertCatalogCommand(ctx, sqlitedb.InsertCatalogCommandParams{
			ID: cid, ProfileID: id, Position: int64(i), Path: cmd.Path, Description: cmd.Description, Params: string(params),
		}); err != nil {
			return fmt.Errorf("store catalog of %s: %w", c.p.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	im.log.Info("profile imported", logging.Event(logging.ProfileImported),
		slog.String("profile", c.p.ID), slog.String("source", c.source), slog.Int("commands", len(c.p.Catalog)))
	if c.overridden && (!exists || cur.Overridden == 0) {
		im.log.Info("custom profile replaces the shipped one", logging.Event(logging.ProfileOverridden),
			slog.String("profile", c.p.ID), slog.String("source", c.source))
	}
	return nil
}

func (im *Importer) pollInterval(ctx context.Context) time.Duration {
	if im.interval > 0 {
		return im.interval
	}
	var v string
	if err := im.st.GlobalSetting(ctx, SettingPollInterval, &v); err == nil {
		if d, err := time.ParseDuration(v); err == nil && d >= MinPollInterval {
			return d
		}
	}
	return DefaultPollInterval
}

// snapshot is the change state of the custom directory: size and
// modification time per file. Sync compares contents by hash.
func (im *Importer) snapshot() map[string]string {
	out := map[string]string{}
	for _, name := range im.customFiles() {
		if fi, err := os.Stat(filepath.Join(im.dir, name)); err == nil {
			out[name] = fmt.Sprintf("%d/%d", fi.Size(), fi.ModTime().UnixNano())
		}
	}
	return out
}

// Watch imports on every change of the custom directory until ctx ends:
// on file events and, as fallback for file systems without events (NFS,
// SMB, Docker Desktop mounts), whenever a poll finds a changed size or
// modification time.
func (im *Importer) Watch(ctx context.Context) {
	if err := os.MkdirAll(im.dir, 0o750); err != nil {
		im.log.Warn("custom profile directory not available", slog.String("dir", im.dir), slog.Any("err", err))
	}
	events := make(<-chan fsnotify.Event)
	if !im.noNotify {
		if w, err := fsnotify.NewWatcher(); err != nil {
			im.log.Warn("file events not available, polling only", slog.Any("err", err))
		} else {
			defer func() { _ = w.Close() }()
			if err := w.Add(im.dir); err != nil {
				im.log.Warn("file events not available, polling only", slog.String("dir", im.dir), slog.Any("err", err))
			} else {
				events = w.Events
			}
		}
	}

	last := im.snapshot()
	poll := time.NewTimer(im.pollInterval(ctx))
	defer poll.Stop()
	var settle <-chan time.Time
	importNow := func(why string) {
		if err := im.Sync(ctx); err != nil && ctx.Err() == nil {
			im.log.Error("profile import failed", slog.String("trigger", why), slog.Any("err", err))
		}
		last = im.snapshot()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-events:
			if !ok {
				events = make(<-chan fsnotify.Event)
				continue
			}
			settle = time.After(debounce)
		case <-settle:
			settle = nil
			importNow("event")
		case <-poll.C:
			if now := im.snapshot(); !maps.Equal(now, last) {
				importNow("poll")
			}
			poll.Reset(im.pollInterval(ctx))
		}
	}
}
