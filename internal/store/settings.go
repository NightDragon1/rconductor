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
// The failure is logged with the key only, never a value.
func (s *Store) GuardedSetting(ctx context.Context, log *slog.Logger, key string, dst any) error {
	v, err := s.Queries.GetSetting(ctx, sqlitedb.GetSettingParams{Key: key, ScopeType: ScopeGlobal, ScopeID: ""})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("read setting %s: %w", key, err)
	case err != nil:
		last, ok := s.kept.Load(key)
		if log != nil {
			log.Error("setting not readable", logging.Event(logging.SettingReadFailed),
				slog.String("setting", key), slog.Bool("kept", ok), slog.Any("err", err))
		}
		if !ok {
			return fmt.Errorf("read setting %s: %w", key, err)
		}
		v = last.(string)
	}
	if err := json.Unmarshal([]byte(v), dst); err != nil {
		return fmt.Errorf("decode setting %s: %w", key, err)
	}
	s.kept.Store(key, v)
	return nil
}
