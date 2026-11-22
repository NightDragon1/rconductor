// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// ScopeGlobal is the scope type of global settings.
const ScopeGlobal = "global"

// TimeLayout is the storage format of all timestamps: UTC, RFC 3339 with
// milliseconds.
const TimeLayout = "2006-01-02T15:04:05.000Z"

// Now returns the current time in storage format.
func Now() string { return time.Now().UTC().Format(TimeLayout) }

// NewID returns a new UUIDv7 for a record (D-027).
func NewID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// EnsureDefault stores value as JSON under the global key unless the key
// already exists. It reports whether it wrote the default.
func (s *Store) EnsureDefault(ctx context.Context, key string, value any) (bool, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return false, fmt.Errorf("encode setting %s: %w", key, err)
	}
	id, err := NewID()
	if err != nil {
		return false, err
	}
	n, err := s.Queries.InsertSettingIfMissing(ctx, sqlitedb.InsertSettingIfMissingParams{
		ID: id, Key: key, Value: string(b), ScopeType: ScopeGlobal, ScopeID: "", UpdatedAt: Now(),
	})
	if err != nil {
		return false, fmt.Errorf("store setting %s: %w", key, err)
	}
	return n > 0, nil
}

// GlobalSettingSince decodes the global setting key into dst and returns
// when it was last changed.
func (s *Store) GlobalSettingSince(ctx context.Context, key string, dst any) (time.Time, error) {
	r, err := s.Queries.GetSettingWithTime(ctx, key)
	if err != nil {
		return time.Time{}, fmt.Errorf("read setting %s: %w", key, err)
	}
	if err := json.Unmarshal([]byte(r.Value), dst); err != nil {
		return time.Time{}, fmt.Errorf("decode setting %s: %w", key, err)
	}
	since, err := time.Parse(TimeLayout, r.UpdatedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("setting %s: %w", key, err)
	}
	return since, nil
}

// GlobalSetting decodes the global setting key into dst.
func (s *Store) GlobalSetting(ctx context.Context, key string, dst any) error {
	v, err := s.Queries.GetSetting(ctx, sqlitedb.GetSettingParams{Key: key, ScopeType: ScopeGlobal, ScopeID: ""})
	if err != nil {
		return fmt.Errorf("read setting %s: %w", key, err)
	}
	if err := json.Unmarshal([]byte(v), dst); err != nil {
		return fmt.Errorf("decode setting %s: %w", key, err)
	}
	return nil
}

// GuardedSetting decodes the global setting key into dst, for a number
// that guards something, such as a time limit or a failure limit (F-93): a
// missing row is an error as in GlobalSetting, so the caller takes its
// default. When the database cannot be read, it decodes the last value read
// before, if any, and returns nil; otherwise the error, so the caller takes
// its default too. Never an extreme value: degrade, do not close (D-077).
// The failure is logged with the key only, never a value; a context that
// ended takes the same way without a log.
func (s *Store) GuardedSetting(ctx context.Context, log *slog.Logger, key string, dst any) error {
	v, err := s.Queries.GetSetting(ctx, sqlitedb.GetSettingParams{Key: key, ScopeType: ScopeGlobal, ScopeID: ""})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("read setting %s: %w", key, err)
	case err != nil:
		last, ok := s.kept.Load(key)
		s.logReadFailed(ctx, log, key, err, "read")
		if !ok {
			return fmt.Errorf("read setting %s: %w", key, err)
		}
		v = last.(string)
	}
	if err := json.Unmarshal([]byte(v), dst); err != nil {
		// A broken row is a failed read too: logged, and the last value
		// read before, if any (D-109).
		s.logReadFailed(ctx, log, key, err, "decode")
		if last, ok := s.kept.Load(key); ok && json.Unmarshal([]byte(last.(string)), dst) == nil {
			return nil
		}
		return fmt.Errorf("decode setting %s: %w", key, err)
	}
	s.kept.Store(key, v)
	return nil
}

// LoggedSetting decodes the global setting key into dst, for a switch
// (D-109, F-93): a failed read or a broken row is logged and returned, never
// a value read before, so the caller keeps the protection on; a missing row
// is an error without a log, as in GlobalSetting.
func (s *Store) LoggedSetting(ctx context.Context, log *slog.Logger, key string, dst any) error {
	err := s.GlobalSetting(ctx, key, dst)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.logReadFailed(ctx, log, key, err, failure(err))
	}
	return err
}

// LoggedSettingSince is LoggedSetting with the time of the last change,
// for a switch that runs out (D-109).
func (s *Store) LoggedSettingSince(ctx context.Context, log *slog.Logger, key string, dst any) (time.Time, error) {
	since, err := s.GlobalSettingSince(ctx, key, dst)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.logReadFailed(ctx, log, key, err, failure(err))
	}
	return since, err
}

// failure names what went wrong without the error text, which can hold the
// value: decode for a broken row, read otherwise.
func failure(err error) string {
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	if errors.As(err, &syn) || errors.As(err, &typ) {
		return "decode"
	}
	return "read"
}

// logReadFailed logs a setting that could not be read, with the key and a
// fixed reason only, never the error text (it can hold the value, SEC-012);
// a request or job that ended is no fault of the database.
func (s *Store) logReadFailed(ctx context.Context, log *slog.Logger, key string, err error, reason string) {
	if log == nil || ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	_, kept := s.kept.Load(key)
	log.Error("setting not readable", logging.Event(logging.SettingReadFailed),
		slog.String("setting", key), slog.Bool("kept", kept), slog.String(logging.KeyReason, reason))
}
