// SPDX-License-Identifier: AGPL-3.0-or-later

// Package cleanup is the daily clean-up job (Design 4.4): it enforces the
// audit retention (AUD-003) and removes expired sessions and invitations
// and soft-deleted records that nothing refers to any more (D-028).
package cleanup

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/NightDragon1/rconductor/internal/auth"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Interval between two runs.
const Interval = 24 * time.Hour

// Result counts what a run removed.
type Result struct {
	Audit, Sessions, Invites, Records int64
}

// Run removes everything that is due at now.
func Run(ctx context.Context, st *store.Store, log *slog.Logger, now time.Time) (Result, error) {
	q := st.Queries
	var res Result
	days := users.DefaultAuditRetentionDays
	if err := st.GlobalSetting(ctx, users.SettingAuditRetentionDays, &days); err != nil || days < users.MinAuditRetentionDays {
		days = users.DefaultAuditRetentionDays
	}
	idle := auth.DefaultIdleTimeout
	var v string
	if err := st.GlobalSetting(ctx, auth.SettingIdleTimeout, &v); err == nil {
		if d, err := time.ParseDuration(v); err == nil && d >= time.Minute {
			idle = d
		}
	}
	now = now.UTC()
	var err error
	cutoff := now.AddDate(0, 0, -days).Format(store.TimeLayout)
	if res.Audit, err = q.PurgeAudit(ctx, cutoff); err != nil {
		return res, fmt.Errorf("purge audit: %w", err)
	}
	log.Info("audit entries removed", logging.Event(logging.AuditPurge),
		slog.Int64("removed", res.Audit), slog.Int("retention_days", days))

	ts := now.Format(store.TimeLayout)
	if res.Sessions, err = q.PurgeSessions(ctx, sqlitedb.PurgeSessionsParams{Now: ts, IdleBefore: now.Add(-idle).Format(store.TimeLayout)}); err != nil {
		return res, fmt.Errorf("purge sessions: %w", err)
	}
	if res.Invites, err = q.PurgeInvites(ctx, ts); err != nil {
		return res, fmt.Errorf("purge invites: %w", err)
	}

	// Records go in an order that frees references first; a record that
	// is still referenced (a foreign key) stays for a later run.
	for _, kind := range []struct {
		list func(context.Context) ([]string, error)
		del  func(context.Context, string) error
	}{
		{q.ListPurgeableGroups, q.DeleteGroupRecord},
		{q.ListPurgeableCommandLists, q.DeleteCommandListRecord},
		{q.ListPurgeableServerGroups, q.DeleteServerGroupRecord},
		{q.ListPurgeableServers, q.DeleteServerRecord},
		{q.ListPurgeableUsers, q.DeleteUserRecord},
	} {
		ids, err := kind.list(ctx)
		if err != nil {
			return res, fmt.Errorf("find deleted records: %w", err)
		}
		for _, id := range ids {
			err := kind.del(ctx, id)
			switch {
			case err == nil:
				res.Records++
			case strings.Contains(err.Error(), "FOREIGN KEY"):
				// still referenced, try again next time
			default:
				return res, fmt.Errorf("remove deleted record: %w", err)
			}
		}
	}
	log.Debug("clean-up done", slog.Int64("sessions", res.Sessions), slog.Int64("invites", res.Invites),
		slog.Int64("records", res.Records))
	return res, nil
}

// Schedule runs the clean-up at start and then every Interval until ctx
// ends. Failures are logged; the next run tries again.
func Schedule(ctx context.Context, st *store.Store, log *slog.Logger) {
	for {
		if _, err := Run(ctx, st, log, time.Now()); err != nil && ctx.Err() == nil {
			log.Error("clean-up failed", slog.Any("err", err))
		}
		t := time.NewTimer(Interval)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}
