// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"context"
	"errors"
	"testing"

	"github.com/NightDragon1/rconductor/internal/users"
)

// TestC413SayPrefixRefusalsAudited (CMD-020, D-082, ROLE-003): reading and
// writing the prefix of an unknown group, of a group outside the scope, and
// as a role without the right is refused and audited once each, with the
// reason; nothing is written.
func TestC413SayPrefixRefusalsAudited(t *testing.T) {
	f, id := sayPrefixWorld(t)
	ctx := context.Background()
	other, err := f.svc.CreateGroup(ctx, f.as("admin"), "other", []string{f.create("c").ID})
	if err != nil {
		t.Fatal(err)
	}
	const unknown = "00000000-0000-4000-8000-000000000413"
	cases := []struct {
		name, who, group string
		want             error
		reason           string
	}{
		{"unknown group", "admin", unknown, users.ErrNotFound, "not_found"},
		{"outside the scope", "sa", other.ID, users.ErrNotFound, "not_found"},
		{"role without the right", "m", id, users.ErrForbidden, "forbidden"},
	}
	for _, c := range cases {
		if _, err := f.svc.SayPrefix(ctx, f.as(c.who), c.group); !errors.Is(err, c.want) {
			t.Errorf("%s, read: %v, want %v", c.name, err, c.want)
		}
		v := int64(1)
		if _, err := f.svc.SetSayPrefix(ctx, f.as(c.who), c.group, "[X]", true, &v); !errors.Is(err, c.want) {
			t.Errorf("%s, write: %v, want %v", c.name, err, c.want)
		}
		for _, action := range []string{actionSayPrefixRead, actionSayPrefixUpdate} {
			if n := f.count(`SELECT count(*) FROM audit_log WHERE action = ? AND result = 'denied' AND reason_code = ? AND target_id = ?`,
				action, c.reason, c.group); n != 1 {
				t.Errorf("%s, %s: %d audited refusals, want 1", c.name, action, n)
			}
		}
	}
	if n := f.count(`SELECT count(*) FROM settings WHERE value LIKE '%[X]%'`); n != 0 {
		t.Errorf("%d prefixes written", n)
	}
}
