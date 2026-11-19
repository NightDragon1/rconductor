// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package messages

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
)

// world is a panel with an RCON admin, two server admins and moderators
// and a supporter; sa and mod1 share the server s1, mod2 has none.
type world struct {
	s                          *Service
	admin, sa, sa2, mod1, mod2 users.Request
	sup                        users.Request
	clock                      time.Time
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{s: newService(t), clock: time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)}
	w.s.now = func() time.Time { return w.clock }
	w.admin, w.sa, w.mod1 = request(t, w.s, perm.RCONAdmin), request(t, w.s, perm.ServerAdmin), request(t, w.s, perm.Moderator)
	w.sa2 = requestAs(t, w.s, perm.ServerAdmin, "sa2")
	w.mod2 = requestAs(t, w.s, perm.Moderator, "mod2")
	w.sup = request(t, w.s, perm.Supporter)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := w.s.st.DB.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO servers (id, name, created_at, updated_at) VALUES ('s1', 's1', 't', 't')`)
	exec(`INSERT INTO admin_scopes (id, user_id, server_id, assigned_by, created_at) VALUES ('as1', ?, 's1', ?, 't')`, w.sa.Actor.ID, w.admin.Actor.ID)
	exec(`INSERT INTO user_groups (id, name, kind, owner_id, created_at, updated_at) VALUES ('g1', 'mods', 'moderator', ?, 't', 't')`, w.sa.Actor.ID)
	exec(`INSERT INTO user_group_members (user_group_id, user_id, added_by, created_at) VALUES ('g1', ?, ?, 't')`, w.mod1.Actor.ID, w.sa.Actor.ID)
	exec(`INSERT INTO grants (id, user_group_id, server_id, granted_by, created_at) VALUES ('gr1', 'g1', 's1', ?, 't')`, w.sa.Actor.ID)
	return w
}

func requestAs(t *testing.T, s *Service, role perm.Role, name string) users.Request {
	t.Helper()
	id := storetest.CreateUser(t, s.st, storetest.User{Pseudonym: name, Role: string(role)})
	return users.Request{Actor: perm.Actor{ID: id, Role: role}, Pseudonym: name}
}

func (w *world) set(t *testing.T, key string, v int) {
	t.Helper()
	if _, err := w.s.st.DB.Exec(`INSERT INTO settings (id, key, value, scope_type, scope_id, updated_at) VALUES (?, ?, ?, 'global', '', 't')
		ON CONFLICT DO UPDATE SET value = excluded.value`, key, key, v); err != nil {
		t.Fatal(err)
	}
}

func (w *world) recipients(t *testing.T, id string) []string {
	t.Helper()
	rows, err := w.s.st.DB.Query(`SELECT user_id FROM message_recipients WHERE message_id = ? ORDER BY user_id`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var u string
		_ = rows.Scan(&u)
		out = append(out, u)
	}
	return out
}

// TestSendAudiences covers MSG-001, MSG-002 and D-085: the recipients are
// resolved at sending time, the sender never among them; a server admin
// reaches the moderators of his scope only.
func TestSendAudiences(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	ids := func(rs ...users.Request) []string {
		var out []string
		for _, r := range rs {
			out = append(out, r.Actor.ID)
		}
		slices.Sort(out)
		return out
	}
	for name, c := range map[string]struct {
		req  users.Request
		in   Input
		want []string
	}{
		"all":              {w.admin, Input{Audience: AudienceAll}, ids(w.sa, w.sa2, w.mod1, w.mod2, w.sup)},
		"server admins":    {w.admin, Input{Audience: AudienceServerAdmins}, ids(w.sa, w.sa2)},
		"moderators":       {w.admin, Input{Audience: AudienceModerators}, ids(w.mod1, w.mod2)},
		"users":            {w.admin, Input{Audience: AudienceUsers, UserIDs: []string{w.mod2.Actor.ID, w.admin.Actor.ID}}, ids(w.mod2)},
		"scope moderators": {w.sa, Input{Audience: AudienceScopeModerators}, ids(w.mod1)},
	} {
		c.in.Text = "hello " + name
		m, err := w.s.Send(ctx, c.req, c.in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := w.recipients(t, m.ID); !slices.Equal(got, c.want) || m.RecipientCount != len(c.want) {
			t.Errorf("%s: recipients %v (%d), want %v", name, got, m.RecipientCount, c.want)
		}
	}
	var inv *users.InvalidError
	for name, c := range map[string]struct {
		req users.Request
		in  Input
	}{
		"no recipient":    {w.sa2, Input{Text: "hi", Audience: AudienceScopeModerators}},
		"unknown user":    {w.admin, Input{Text: "hi", Audience: AudienceUsers, UserIDs: []string{"00000000-0000-4000-8000-000000000009"}}},
		"only the sender": {w.admin, Input{Text: "hi", Audience: AudienceUsers, UserIDs: []string{w.admin.Actor.ID}}},
		"text too long":   {w.admin, Input{Text: strings.Repeat("ä", users.DefaultMessagesMaxText+1), Audience: AudienceAll}},
	} {
		if _, err := w.s.Send(ctx, c.req, c.in); !errors.As(err, &inv) {
			t.Errorf("%s = %v, want invalid", name, err)
		}
	}
	w.set(t, users.SettingMessagesMaxResolved, 2)
	if _, err := w.s.Send(ctx, w.admin, Input{Text: "hi", Audience: AudienceAll}); !errors.Is(err, ErrTooManyRecipients) {
		t.Errorf("over max_resolved_recipients = %v", err)
	}
	var payloads string
	_ = w.s.st.DB.QueryRow(`SELECT group_concat(payload, '|') FROM audit_log WHERE action = 'message.sent'`).Scan(&payloads)
	if strings.Count(payloads, `"recipients":`) != 5 || strings.Contains(payloads, "hello") {
		t.Errorf("audit: %s", payloads)
	}
}

// TestSendRateLimit covers MSG-001: messages.per_hour counts the sends
// of the last hour; an hour later the sender may send again.
func TestSendRateLimit(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.set(t, users.SettingMessagesPerHour, 2)
	in := Input{Text: "hi", Audience: AudienceModerators}
	for range 2 {
		if _, err := w.s.Send(ctx, w.admin, in); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.s.Send(ctx, w.admin, in); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("third = %v", err)
	}
	w.clock = w.clock.Add(time.Hour + time.Second)
	if _, err := w.s.Send(ctx, w.admin, in); err != nil {
		t.Errorf("an hour later = %v", err)
	}
}

// TestSendParallel: of parallel sends past messages.per_hour exactly the
// allowed number passes.
func TestSendParallel(t *testing.T) {
	w := newWorld(t)
	w.set(t, users.SettingMessagesPerHour, 5)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, limited := 0, 0
	for range 12 {
		wg.Go(func() {
			_, err := w.s.Send(context.Background(), w.admin, Input{Text: "hi", Audience: AudienceAll})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrRateLimited):
				limited++
			default:
				t.Errorf("send: %v", err)
			}
		})
	}
	wg.Wait()
	if ok != 5 || limited != 7 {
		t.Errorf("ok %d, limited %d", ok, limited)
	}
}
