// SPDX-License-Identifier: AGPL-3.0-or-later

package backup

import (
	"context"
	"time"

	"github.com/NightDragon1/rconductor/internal/store"
)

// BeforeMigrate returns the store hook that writes a pre-migration backup
// of an existing database (NFR-013). The settings table exists from the
// first schema version on, so the policy can be read before migrating.
func BeforeMigrate(s *store.Store, b *Backuper) func(ctx context.Context, from, to int64) error {
	return func(ctx context.Context, from, _ int64) error {
		_, err := b.Run(ctx, KindPreMigrate, from, LoadPolicy(ctx, s, b.Log))
		return err
	}
}

// Schedule writes automatic backups until ctx ends. The interval is read
// from settings before each wait, so changes apply without a restart. The
// first backup is due one interval after the newest automatic backup, or
// one interval after start if there is none.
func Schedule(ctx context.Context, s *store.Store, b *Backuper) {
	last := LastAuto(b.Dir)
	if last.IsZero() {
		last = b.now()
	}
	for {
		p := LoadPolicy(ctx, s, b.Log)
		wait := time.Until(last.Add(p.Interval))
		if wait < 0 {
			wait = 0
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		// A failure is logged by Run; retry after a full interval.
		_, _ = b.Run(ctx, KindAuto, 0, p)
		last = b.now()
	}
}
