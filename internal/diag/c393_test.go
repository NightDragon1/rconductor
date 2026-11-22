// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package diag

import (
	"context"
	"strconv"
	"testing"

	"github.com/NightDragon1/rconductor/internal/diag/diagconf"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
)

// TestC393StoredBoundsOutOfRange (NFR-018, D-090): a size or a number of
// exports stored past the upper end, written around the settings check,
// falls back to the default in the export.
func TestC393StoredBoundsOutOfRange(t *testing.T) {
	st := storetest.New(t)
	for key, v := range map[string]int{
		users.SettingDiagnosticsMaxBytes: diagconf.MaxMaxBytes + 1,
		users.SettingDiagnosticsParallel: diagconf.MaxParallel + 1,
	} {
		if _, err := st.DB.Exec(`INSERT INTO settings (id, key, value, updated_at) VALUES (?, ?, ?, 't')
			ON CONFLICT DO UPDATE SET value = excluded.value`, "s-"+key, key, strconv.Itoa(v)); err != nil {
			t.Fatal(err)
		}
	}
	s := NewService(st, nil)
	ctx := context.Background()
	if n := s.maxBytes(ctx); n != diagconf.DefaultMaxBytes {
		t.Errorf("max bytes = %d, want the default", n)
	}
	var held []func()
	for range diagconf.DefaultParallel {
		release, ok := s.acquire(ctx)
		if !ok {
			t.Fatal("a slot within the default was refused")
		}
		held = append(held, release)
	}
	if _, ok := s.acquire(ctx); ok {
		t.Error("one export more than the default was let through")
	}
	for _, release := range held {
		release()
	}
}
