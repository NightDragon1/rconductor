// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/connmgr"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logtest"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/rcon"
	"github.com/NightDragon1/rconductor/internal/rcon/rcontest"
	"github.com/NightDragon1/rconductor/internal/rcon/source"
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
	s, err := f.svc.Update(ctx, f.as("admin"), s.ID, Input{GamePorts: &over, Version: f.version(s.ID)})
	if err != nil || !s.GamePortsOverride || s.GamePorts[0].Port != 25566 {
		t.Fatalf("override = %+v %v", s, err)
	}
	none := []profiles.GamePort{}
	if s, _ = f.svc.Update(ctx, f.as("admin"), s.ID, Input{GamePorts: &none, Version: f.version(s.ID)}); s.GamePortsOverride || s.GamePorts[0].Port != 25565 {
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
	logtest.Wait(t, f.logs, `"event":"perm.denied"`)
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
	logtest.Wait(t, f.logs, "enter them again")
	got, _ := other.Get(ctx, f.as("admin"), s.ID)
	if got.Credentials != CredentialsReenter {
		t.Errorf("credentials state = %s", got.Credentials)
	}
	if _, err := other.Credentials(ctx, s.ID); !errors.Is(err, secret.ErrOtherKey) {
		t.Errorf("open with another key: %v", err)
	}
	pw := logging.Secret("new password")
	if got, _ = other.Update(ctx, f.as("admin"), s.ID, Input{Password: &pw, Version: f.version(s.ID)}); got.Credentials != CredentialsOK {
		t.Errorf("after entering again: %s", got.Credentials)
	}
}

// TestConnectsWithStoredCredentials: a created server is connected with
// its decrypted password, and its status shows in the server answer
// (SRV-004); a new password reconnects at once.
func TestConnectsWithStoredCredentials(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, err := rcontest.Start(rconPassword, rcontest.Minecraft, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	m := connmgr.New(rcon.Registry{"source-rcon": source.Adapter{}}, f.svc, f.svc.log, connmgr.Options{ProbeInterval: 50 * time.Millisecond, MaxBackoff: 100 * time.Millisecond})
	f.svc.SetConnections(m)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer m.Wait()
	defer cancel()

	host, port, _ := net.SplitHostPort(srv.Addr)
	p, _ := strconv.Atoi(port)
	name, wrong, profile := "local", logging.Secret("wrong"), f.ids["profile"]
	s, err := f.svc.Create(ctx, f.as("admin"), Input{Name: &name, ProfileID: &profile, Host: &host, Port: &p, Password: &wrong})
	if err != nil {
		t.Fatal(err)
	}
	waitFor := func(want connmgr.Status) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			got, _ := f.svc.Get(ctx, f.as("admin"), s.ID)
			if got.Status == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("status %s, want %s", got.Status, want)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitFor(connmgr.AuthFailed)
	right := logging.Secret(rconPassword)
	if _, err := f.svc.Update(ctx, f.as("admin"), s.ID, Input{Password: &right, Version: f.version(s.ID)}); err != nil {
		t.Fatal(err)
	}
	waitFor(connmgr.Connected)
	if got, err := m.Execute(ctx, s.ID, "list"); err != nil || got != "list" {
		t.Errorf("execute = %q %v", got, err)
	}
	if err := f.svc.Delete(ctx, f.as("admin"), s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Execute(ctx, s.ID, "list"); !errors.Is(err, connmgr.ErrUnknownServer) {
		t.Errorf("deleted server still connected: %v", err)
	}
}

// version reads the stored version of a server (revision 2.4 candidate
// 24).
func (f *fixture) version(id string) *int64 {
	f.t.Helper()
	var v int64
	if err := f.st.DB.QueryRowContext(context.Background(), `SELECT version FROM servers WHERE id = ?`, id).Scan(&v); err != nil {
		f.t.Fatal(err)
	}
	return &v
}

// connectionFixture is a running manager with a server on a test RCON
// server, for the connection test (SRV-007).
func connectionFixture(t *testing.T) (*fixture, *rcontest.Server, ConnectionData) {
	t.Helper()
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	srv, err := rcontest.Start(rconPassword, rcontest.Minecraft, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	m := connmgr.New(rcon.Registry{"source-rcon": source.Adapter{}}, f.svc, f.svc.log, connmgr.Options{ProbeInterval: 50 * time.Millisecond, MaxBackoff: 100 * time.Millisecond})
	f.svc.SetConnections(m)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Wait)
	t.Cleanup(cancel)
	host, port, _ := net.SplitHostPort(srv.Addr)
	p, _ := strconv.Atoi(port)
	return f, srv, ConnectionData{ProfileID: &[]string{f.ids["profile"]}[0], Host: &host, Port: &p}
}

func secretOf(s string) *logging.Secret {
	v := logging.Secret(s)
	return &v
}

// TestConnectionNewData covers SRV-007: new data get a short connection,
// nothing is stored, only the RCON admin may test, and the password is in
// neither log nor audit.
func TestConnectionNewData(t *testing.T) {
	f, srv, in := connectionFixture(t)
	ctx := context.Background()
	test := func(pw string, want string) {
		t.Helper()
		in.Password = secretOf(pw)
		if got, err := f.svc.TestConnection(ctx, f.as("admin"), in); err != nil || got != want {
			t.Errorf("password %q: %q %v, want %q", pw, got, err, want)
		}
	}
	test(rconPassword, ConnectionOK)
	test("wrong", ConnectionAuthFailed)
	if srv.Commands() != 0 {
		t.Errorf("%d commands sent", srv.Commands())
	}
	closed := "127.0.0.1"
	port := 1
	in.Host, in.Port = &closed, &port
	test(rconPassword, ConnectionUnreachable)

	var n int
	_ = f.st.DB.QueryRowContext(ctx, `SELECT count(*) FROM servers`).Scan(&n)
	if n != 0 {
		t.Errorf("%d servers stored", n)
	}
	if _, err := f.svc.TestConnection(ctx, f.as("sa"), in); !errors.Is(err, users.ErrForbidden) {
		t.Errorf("server admin: %v", err)
	}
	if strings.Contains(f.logs.String(), rconPassword) {
		t.Error("password in the log")
	}
	var entries int
	_ = f.st.DB.QueryRowContext(ctx, `SELECT count(*) FROM audit_log WHERE action = 'server.connection_test' AND result = 'ok' AND payload LIKE '%result%'`).Scan(&entries)
	if entries != 3 {
		t.Errorf("%d audit entries", entries)
	}
	_ = f.st.DB.QueryRowContext(ctx, `SELECT count(*) FROM audit_log WHERE payload LIKE ?`, "%"+rconPassword+"%").Scan(&entries)
	if entries != 0 {
		t.Error("password in the audit")
	}
	if !strings.Contains(f.logs.String(), `"event":"rcon.connection_test"`) {
		t.Error("no event")
	}
	in.Password = nil
	var inv *users.InvalidError
	if _, err := f.svc.TestConnection(ctx, f.as("admin"), in); !errors.As(err, &inv) {
		t.Errorf("no password: %v", err)
	}
}

// TestConnectionRunning covers PROT-002 rev. 2 and D-049: unchanged data
// connect no second time, also with the password typed in again; changed
// data do, and at a new address the stored password is used (D-050).
func TestConnectionRunning(t *testing.T) {
	f, srv, in := connectionFixture(t)
	ctx := context.Background()
	name := "local"
	sv, err := f.svc.Create(ctx, f.as("admin"), Input{Name: &name, ProfileID: in.ProfileID, Host: in.Host, Port: in.Port, Password: secretOf(rconPassword)})
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if st, _ := f.svc.conns.Status(sv.ID); st == connmgr.Connected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("not connected")
		}
	}
	before := srv.Connections()
	for _, pw := range []*logging.Secret{nil, secretOf(rconPassword)} {
		got, err := f.svc.TestConnection(ctx, f.as("admin"), ConnectionData{ServerID: sv.ID, Password: pw, Host: in.Host, Port: in.Port})
		if err != nil || got != ConnectionOK {
			t.Errorf("unchanged: %q %v", got, err)
		}
	}
	if n := srv.Connections(); n != before {
		t.Errorf("%d connections after unchanged data, had %d", n, before)
	}

	got, err := f.svc.TestConnection(ctx, f.as("admin"), ConnectionData{ServerID: sv.ID, Password: secretOf("wrong")})
	if err != nil || got != ConnectionAuthFailed {
		t.Errorf("changed password: %q %v", got, err)
	}
	if n := srv.Connections(); n != before+1 {
		t.Errorf("%d connections after changed data, had %d", n, before)
	}

	// A listener that records what arrives stands for the new address.
	other, err := rcontest.Start(rconPassword, rcontest.Minecraft, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	host, port, _ := net.SplitHostPort(other.Addr)
	p, _ := strconv.Atoi(port)
	if got, err := f.svc.TestConnection(ctx, f.as("admin"), ConnectionData{ServerID: sv.ID, Host: &host, Port: &p}); err != nil || got != ConnectionOK {
		t.Errorf("new address with the stored password: %q %v", got, err)
	}
	if other.Connections() != 1 {
		t.Errorf("%d connections at the new address, want 1", other.Connections())
	}
	var inv *users.InvalidError
	if _, err := f.svc.TestConnection(ctx, f.as("admin"), ConnectionData{ProfileID: in.ProfileID, Host: &host, Port: &p}); !errors.As(err, &inv) {
		t.Errorf("without server and password: %v", err)
	}
}

// TestHostChangeKeepsPassword covers D-050: the RCON admin moves a server
// to a new host and port without typing the password again; the stored
// one connects there, and the audit names the fields, never the password.
func TestHostChangeKeepsPassword(t *testing.T) {
	f, _, in := connectionFixture(t)
	ctx := context.Background()
	name := "moved"
	sv, err := f.svc.Create(ctx, f.as("admin"), Input{Name: &name, ProfileID: in.ProfileID, Host: in.Host, Port: in.Port, Password: secretOf(rconPassword)})
	if err != nil {
		t.Fatal(err)
	}
	other, err := rcontest.Start(rconPassword, rcontest.Minecraft, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	host, port, _ := net.SplitHostPort(other.Addr)
	p, _ := strconv.Atoi(port)
	got, err := f.svc.Update(ctx, f.as("admin"), sv.ID, Input{Host: &host, Port: &p, Version: f.version(sv.ID)})
	if err != nil {
		t.Fatalf("host and port without password: %v", err)
	}
	if got.Host != host || got.Port != p || got.Credentials != "ok" {
		t.Errorf("after the move: %+v", got)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if st, _ := f.svc.conns.Status(sv.ID); st == connmgr.Connected && other.Connections() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("not connected at the new address with the stored password")
		}
	}
	var payload string
	if err := f.st.DB.QueryRowContext(ctx, `SELECT payload FROM audit_log WHERE action = 'server.update' AND target_id = ?`, sv.ID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	// Both listeners run on 127.0.0.1, so only the port moves (B10-1).
	if !strings.Contains(payload, `"port":{"from"`) || strings.Contains(payload, "host") || strings.Contains(payload, rconPassword) {
		t.Errorf("audit payload = %s", payload)
	}
}

// TestConnectionLimit: the limit counts per user and starts again after a
// minute; a refused test is audited and reaches nothing.
func TestConnectionLimit(t *testing.T) {
	var l testLimiter
	now := time.Now()
	for range 3 {
		if !l.take("a", 3, now) {
			t.Fatal("refused too early")
		}
	}
	if l.take("a", 3, now.Add(59*time.Second)) {
		t.Error("over the limit")
	}
	if !l.take("b", 3, now) {
		t.Error("the limit of another user counted")
	}
	if !l.take("a", 3, now.Add(time.Minute)) {
		t.Error("no new window")
	}

	f, srv, in := connectionFixture(t)
	ctx := context.Background()
	in.Password = secretOf(rconPassword)
	var wg sync.WaitGroup
	var limited atomic.Int64
	for range 2 * users.DefaultConnectionTests {
		wg.Go(func() {
			if _, err := f.svc.TestConnection(ctx, f.as("admin"), in); errors.Is(err, ErrTestRateLimited) {
				limited.Add(1)
			}
		})
	}
	wg.Wait()
	if limited.Load() != users.DefaultConnectionTests || srv.Connections() != users.DefaultConnectionTests {
		t.Errorf("limited %d, connections %d", limited.Load(), srv.Connections())
	}
}

// TestB4PasswordFromTheReadOfTheAddress: a change after the address was read
// does not reach the password of that test. This cannot fail on the code
// before the fix: the race sat between two reads inside TestConnection, with
// no seam for a test. It guards the part that closes it.
func TestB4PasswordFromTheReadOfTheAddress(t *testing.T) {
	f, _, in := connectionFixture(t)
	ctx := context.Background()
	name := "local"
	sv, err := f.svc.Create(ctx, f.as("admin"), Input{Name: &name, ProfileID: in.ProfileID, Host: in.Host, Port: in.Port, Password: secretOf(rconPassword)})
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.st.Queries.GetServer(ctx, sv.ID)
	if err != nil {
		t.Fatal(err)
	}
	host := "other.lan"
	var version int64
	if err := f.st.DB.QueryRowContext(ctx, `SELECT version FROM servers WHERE id = ?`, sv.ID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Update(ctx, f.as("admin"), sv.ID, Input{Host: &host, Password: secretOf("for the new address"), Version: &version}); err != nil {
		t.Fatal(err)
	}
	if pw := f.svc.storedPassword(sv.ID, r.Secret); pw != rconPassword {
		t.Errorf("password of the read = %q", pw)
	}
	if pw := f.svc.storedPassword(sv.ID, []byte("broken")); pw != "" {
		t.Errorf("broken secret = %q", pw)
	}
}
