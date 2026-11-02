// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/secret"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
)

const rconPassword = "rcon-Secret-4711"

type fixture struct {
	t    *testing.T
	st   *store.Store
	svc  *Service
	c    *perm.Checker
	logs *bytes.Buffer
	ids  map[string]string
}

func newBox(t *testing.T) *secret.Box {
	t.Helper()
	k := make([]byte, 32)
	_, _ = rand.Read(k)
	b, err := secret.New(k)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st := storetest.New(t)
	logs := &bytes.Buffer{}
	log := logging.New(logs, "panel", config.Log{Format: "json"}).Module("servers")
	c := perm.NewChecker(st, log)
	f := &fixture{t: t, st: st, c: c, logs: logs, svc: NewService(st, c, newBox(t), log), ids: map[string]string{}}
	f.ids["admin"] = storetest.CreateUser(t, st, storetest.User{Pseudonym: "admin", Role: "rcon_admin"})
	f.ids["sa"] = storetest.CreateUser(t, st, storetest.User{Pseudonym: "sa", Role: "server_admin"})
	f.ids["m"] = storetest.CreateUser(t, st, storetest.User{Pseudonym: "m", Role: "moderator"})
	imp := profiles.NewImporter(st, log, os.DirFS("../../profiles"), t.TempDir())
	if err := imp.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	var profile string
	if err := st.DB.QueryRowContext(context.Background(), `SELECT id FROM game_profiles WHERE slug = 'minecraft-java'`).Scan(&profile); err != nil {
		t.Fatal(err)
	}
	f.ids["profile"] = profile
	return f
}

func (f *fixture) as(name string) users.Request {
	f.t.Helper()
	r, err := f.c.For(context.Background(), f.ids[name])
	if err != nil {
		f.t.Fatal(err)
	}
	return users.Request{Actor: perm.Actor{ID: f.ids[name], Role: r.Role}, Pseudonym: name, RequestID: "req"}
}

func (f *fixture) create(name string) Server {
	f.t.Helper()
	host, pw, profile := "mc.lan", logging.Secret(rconPassword), f.ids["profile"]
	s, err := f.svc.Create(context.Background(), f.as("admin"), Input{Name: &name, ProfileID: &profile, Host: &host, Password: &pw})
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}

func (f *fixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.st.DB.ExecContext(context.Background(), q, args...); err != nil {
		f.t.Fatal(err)
	}
}

// TestCreateServer covers SRV-001 and SRV-005: required fields, port and
// game ports from the profile, an override and its removal.
func TestCreateServer(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.create("Survival")
	if s.Port != 25575 || s.Protocol != "source-rcon" || s.Credentials != CredentialsOK || !s.Detailed {
		t.Errorf("server = %+v", s)
	}
	if len(s.GamePorts) != 1 || s.GamePorts[0].Port != 25565 || s.GamePortsOverride {
		t.Errorf("game ports from the profile = %+v", s.GamePorts)
	}
	over := []profiles.GamePort{{Port: 25566, Proto: "udp", Name: "voice"}}
	s, err := f.svc.Update(ctx, f.as("admin"), s.ID, Input{GamePorts: &over})
	if err != nil || !s.GamePortsOverride || s.GamePorts[0].Port != 25566 {
		t.Fatalf("override = %+v %v", s, err)
	}
	none := []profiles.GamePort{}
	if s, _ = f.svc.Update(ctx, f.as("admin"), s.ID, Input{GamePorts: &none}); s.GamePortsOverride || s.GamePorts[0].Port != 25565 {
		t.Errorf("back to the profile = %+v", s.GamePorts)
	}

	var inv *users.InvalidError
	name := "Other"
	for what, in := range map[string]Input{
		"missing password": {Name: &name, ProfileID: new(f.ids["profile"]), Host: new("mc.lan")},
		"bad host":         {Name: &name, ProfileID: new(f.ids["profile"]), Host: new("mc lan"), Password: new(logging.Secret("x"))},
		"unknown profile":  {Name: &name, ProfileID: new("nope"), Host: new("mc.lan"), Password: new(logging.Secret("x"))},
		"duplicate name":   {Name: new("survival"), ProfileID: new(f.ids["profile"]), Host: new("mc.lan"), Password: new(logging.Secret("x"))},
		"bad port":         {Name: &name, ProfileID: new(f.ids["profile"]), Host: new("mc.lan"), Port: new(70000), Password: new(logging.Secret("x"))},
	} {
		if _, err := f.svc.Create(ctx, f.as("admin"), in); !errors.As(err, &inv) {
			t.Errorf("%s: %v", what, err)
		}
	}
	if _, err := f.svc.Create(ctx, f.as("sa"), Input{}); !errors.Is(err, users.ErrForbidden) {
		t.Errorf("server admin creates a server: %v", err)
	}
}

// TestScope covers ROLE-003: a server admin sees exactly the servers of his
// groups and single servers; others answer not found, logged and audited.
func TestScope(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a, b, c := f.create("a"), f.create("b"), f.create("c")
	g, err := f.svc.CreateGroup(ctx, f.as("admin"), "mc", []string{a.ID, b.ID})
	if err != nil {
		t.Fatal(err)
	}
	// SRV-003: a server can be in several groups.
	if _, err := f.svc.CreateGroup(ctx, f.as("admin"), "all", []string{a.ID, b.ID, c.ID}); err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO admin_scopes (id, user_id, server_group_id, assigned_by, created_at) VALUES ('x1', ?, ?, ?, 't')`, f.ids["sa"], g.ID, f.ids["admin"])
	f.c.Invalidate()

	list, err := f.svc.List(ctx, f.as("sa"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range list {
		names = append(names, s.Name)
		if slices.Contains(s.GroupIDs, "") || len(s.GroupIDs) != 1 {
			t.Errorf("%s shows groups %v; only the group in scope is visible", s.Name, s.GroupIDs)
		}
	}
	if !slices.Equal(names, []string{"a", "b"}) {
		t.Errorf("server admin sees %v", names)
	}
	if _, err := f.svc.Get(ctx, f.as("sa"), c.ID); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("server outside the scope: %v", err)
	}
	if !strings.Contains(f.logs.String(), `"event":"perm.denied"`) {
		t.Error("perm.denied missing")
	}
	gs, _ := f.svc.Groups(ctx, f.as("sa"))
	if len(gs) != 1 || gs[0].Name != "mc" {
		t.Errorf("server admin sees groups %+v", gs)
	}
	all, _ := f.svc.Groups(ctx, f.as("admin"))
	if len(all) != 2 {
		t.Errorf("RCON admin sees %d groups", len(all))
	}
	if list, _ := f.svc.List(ctx, f.as("m")); len(list) != 0 {
		t.Errorf("moderator without grants sees %d servers", len(list))
	}
}

func TestModeratorSeesNoConnectionFields(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.create("mc")
	f.exec(`INSERT INTO user_groups (id, name, kind, owner_id, created_at, updated_at) VALUES ('mg', 'mg', 'moderator', ?, 't', 't')`, f.ids["admin"])
	f.exec(`INSERT INTO user_group_members (user_group_id, user_id, added_by, created_at) VALUES ('mg', ?, ?, 't')`, f.ids["m"], f.ids["admin"])
	f.exec(`INSERT INTO grants (id, user_group_id, server_id, granted_by, created_at) VALUES ('g', 'mg', ?, ?, 't')`, s.ID, f.ids["admin"])
	f.c.Invalidate()
	got, err := f.svc.Get(ctx, f.as("m"), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Detailed || got.Host != "" || got.Port != 0 || got.Credentials != "" {
		t.Errorf("moderator sees connection fields: %+v", got)
	}
}

// TestOtherMasterKey covers SRV-002: after a new master key the panel
// starts, flags the server and asks for the credentials again.
func TestOtherMasterKey(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.create("mc")
	if got, err := f.svc.Credentials(ctx, s.ID); err != nil || got.Reveal() != rconPassword {
		t.Fatalf("credentials = %v", err)
	}
	var stored []byte
	_ = f.st.DB.QueryRowContext(ctx, `SELECT secret FROM servers WHERE id = ?`, s.ID).Scan(&stored)
	if bytes.Contains(stored, []byte(rconPassword)) {
		t.Error("password stored in plain")
	}

	other := NewService(f.st, f.c, newBox(t), f.svc.log)
	if err := other.CheckCredentials(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.logs.String(), "enter them again") {
		t.Error("no hint at start")
	}
	got, _ := other.Get(ctx, f.as("admin"), s.ID)
	if got.Credentials != CredentialsReenter {
		t.Errorf("credentials state = %s", got.Credentials)
	}
	if _, err := other.Credentials(ctx, s.ID); !errors.Is(err, secret.ErrOtherKey) {
		t.Errorf("open with another key: %v", err)
	}
	pw := logging.Secret("new password")
	if got, _ = other.Update(ctx, f.as("admin"), s.ID, Input{Password: &pw}); got.Credentials != CredentialsOK {
		t.Errorf("after entering again: %s", got.Credentials)
	}
}
