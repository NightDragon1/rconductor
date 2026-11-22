// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"errors"
	"testing"

	"github.com/NightDragon1/rconductor/internal/users"
)

// TestC447RateLimitBeforeThePrefix (CMD-019, CMD-020, D-051): a say over
// the rate limit is refused as rate_limited before the prefix of its server
// groups is read; role and scope still come first.
func TestC447RateLimitBeforeThePrefix(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	lim := w.svc.limit(ctx, "rcon_admin")
	for range lim.Burst {
		w.svc.limiter.take(w.ids["admin"], w.server, lim, w.svc.now())
	}
	// Outside the scope the answer stays not found, also with a spent
	// bucket: the moderator on a server that exists but is not his.
	other := w.second(t, "other")
	w.exec(`DELETE FROM admin_scopes WHERE id = 'as-other'`)
	w.c.Invalidate()
	mlim := w.svc.limit(ctx, "moderator")
	for range mlim.Burst {
		w.svc.limiter.take(w.ids["m"], other, mlim, w.svc.now())
	}
	var denied *DeniedError
	if _, err := w.svc.Send(ctx, w.as("m"), other, "say hi"); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("outside the scope with a spent bucket: %v", err)
	}
	// The prefix cannot be read any more; a read now would fail the say.
	w.exec(`ALTER TABLE server_group_members RENAME TO server_group_members_gone`)
	_, err := w.send("admin", "say hi")
	if !errors.As(err, &denied) || denied.Reason != ReasonRateLimited {
		t.Fatalf("say over the limit: %v", err)
	}
}
