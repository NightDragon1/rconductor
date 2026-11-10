// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/servers"
	"github.com/NightDragon1/rconductor/internal/users"
)

// second adds a server without a game behind it, in the scope of sa.
func (w *world) second(t *testing.T, name string) string {
	t.Helper()
	var profile string
	_ = w.st.DB.QueryRow(`SELECT id FROM game_profiles WHERE slug = 'minecraft-java'`).Scan(&profile)
	host, port, pw := "127.0.0.1", 1, logging.Secret("x")
	s, err := w.srv.Create(context.Background(), w.as("admin"), servers.Input{Name: &name, ProfileIDs: &[]string{profile}, Host: &host, Port: &port, Password: &pw})
	if err != nil {
		t.Fatal(err)
	}
	w.exec(`INSERT INTO admin_scopes (id, user_id, server_id, assigned_by, created_at) VALUES (?, ?, ?, ?, 't')`, "as-"+name, w.ids["sa"], s.ID, w.ids["admin"])
	w.c.Invalidate()
	return s.ID
}

func (w *world) setting(key string, v int) {
	w.exec(`INSERT INTO settings (id, key, value, updated_at) VALUES (?, ?, ?, 't')
		ON CONFLICT DO UPDATE SET value = excluded.value`, "set-"+key, key, strconv.Itoa(v))
}

// TestMultiPerServer covers CMD-010 and D-069: one run per server with
// its own request ID and a shared batch ID; a server outside the scope and
// an unknown ID both give not_found; one failing server leaves the others.
func TestMultiPerServer(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	other := w.second(t, "other")
	outside := w.second(t, "outside")
	w.exec(`DELETE FROM admin_scopes WHERE id = 'as-outside'`)
	w.c.Invalidate()
	ids := []string{w.server, other, outside, "01900000-0000-7000-8000-000000000000", w.server}
	batch, items, err := w.svc.Multi(ctx, w.as("sa"), ids, "say hi")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("items = %+v, want 4 (duplicates folded)", items)
	}
	want := []string{ResultOK, ResultError, ResultNotFound, ResultNotFound}
	seenReq := map[string]bool{}
	for i, it := range items {
		if it.Result != want[i] {
			t.Errorf("%d: %+v, want %s", i, it, want[i])
		}
		if it.Result == ResultNotFound && (it.RequestID != "" || it.Reason != "not_found") {
			t.Errorf("not_found tells more: %+v", it)
		}
		if it.RequestID != "" {
			if seenReq[it.RequestID] {
				t.Errorf("request ID twice: %s", it.RequestID)
			}
			seenReq[it.RequestID] = true
		}
	}
	if items[0].Response != "ok: say hi" {
		t.Errorf("answer = %q", items[0].Response)
	}
	// Audit per server, all with the batch ID.
	rows, err := w.st.DB.Query(`SELECT server_id, action, payload FROM audit_log WHERE kind = 'command' AND action IN ('cmd.sent', 'cmd.result')`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	perServer := map[string]int{}
	for rows.Next() {
		var server, action, raw string
		if err := rows.Scan(&server, &action, &raw); err != nil {
			t.Fatal(err)
		}
		var p struct {
			BatchID string `json:"batch_id"`
		}
		_ = json.Unmarshal([]byte(raw), &p)
		if p.BatchID != batch {
			t.Errorf("%s on %s: batch %q, want %q", action, server, p.BatchID, batch)
		}
		perServer[server]++
	}
	if perServer[w.server] != 2 || perServer[other] != 2 || perServer[outside] != 0 {
		t.Errorf("audit entries per server = %v", perServer)
	}
}

// TestMultiOnlyAdmins: moderator and supporter are refused before
// anything is looked up.
func TestMultiOnlyAdmins(t *testing.T) {
	w := newWorld(t)
	for _, role := range []string{"m", "sup"} {
		if _, _, err := w.svc.Multi(context.Background(), w.as(role), []string{w.server}, "say hi"); !errors.Is(err, users.ErrForbidden) {
			t.Errorf("%s: %v", role, err)
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.received) != 0 {
		t.Errorf("the game got %v", w.received)
	}
}

// TestMultiLimits: no server, too many servers; the setting and its test
// value come from the test setup.
func TestMultiLimits(t *testing.T) {
	w := newWorld(t)
	var inv *users.InvalidError
	if _, _, err := w.svc.Multi(context.Background(), w.as("admin"), nil, "say hi"); !errors.As(err, &inv) {
		t.Errorf("no server: %v", err)
	}
	if _, _, err := w.svc.Multi(context.Background(), w.as("admin"), []string{w.server}, "  "); !errors.As(err, &inv) {
		t.Errorf("no command: %v", err)
	}
	w.setting(users.SettingMultiMaxServers, 2)
	ids := []string{w.server, w.second(t, "b"), w.second(t, "c")}
	if _, _, err := w.svc.Multi(context.Background(), w.as("admin"), ids, "say hi"); !errors.As(err, &inv) || !strings.Contains(inv.Detail, "2") {
		t.Errorf("three servers with a limit of 2: %v", err)
	}
	if _, items, err := w.svc.Multi(context.Background(), w.as("admin"), ids[:2], "say hi"); err != nil || len(items) != 2 {
		t.Errorf("two servers: %v %v", items, err)
	}
}

// TestMultiRateLimitPerBucket covers D-069 with Design 3.4: each run takes
// one token of the bucket (user, server), like a single command; a single
// command afterwards sees the lower count.
func TestMultiRateLimitPerBucket(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	w.svc.now = func() time.Time { return now }
	w.setting("ratelimit.server_admin.burst", 2)
	w.setting("ratelimit.server_admin.per_minute", 1)
	other := w.second(t, "other")
	if _, items, err := w.svc.Multi(ctx, w.as("sa"), []string{w.server, other}, "say hi"); err != nil || items[0].Result != ResultOK || items[1].Reason == ReasonRateLimited {
		t.Fatalf("multi = %+v %v", items, err)
	}
	// One token left on each: one single command passes, the next not.
	var denied *DeniedError
	if _, err := w.svc.Send(ctx, w.as("sa"), w.server, "say one"); err != nil {
		t.Errorf("first single after the multi: %v", err)
	}
	if _, err := w.svc.Send(ctx, w.as("sa"), w.server, "say two"); !errors.As(err, &denied) || denied.Reason != ReasonRateLimited {
		t.Errorf("second single after the multi: %v", err)
	}
	// The other bucket lost exactly one token too.
	_, items, _ := w.svc.Multi(ctx, w.as("sa"), []string{other}, "say hi")
	if items[0].Reason == ReasonRateLimited {
		t.Errorf("other bucket empty after one run: %+v", items[0])
	}
	_, items, _ = w.svc.Multi(ctx, w.as("sa"), []string{other, w.server}, "say hi")
	for _, it := range items {
		if it.Reason != ReasonRateLimited {
			t.Errorf("bucket of %s not empty: %+v", it.ServerID, it)
		}
	}
}
