// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/NightDragon1/rconductor/internal/logtest"
	"github.com/NightDragon1/rconductor/internal/rcon/rcontest"
)

// The tests here close the gaps of the acceptance run in M5: each proves a
// part of a criterion against a running panel, over its API.

func (f *rightsFixture) db() *sql.DB {
	f.t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(f.data, "rconductor.db")+"?_pragma=busy_timeout(5000)")
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = db.Close() })
	return db
}

// send posts a command as role and returns the answer with its headers.
func (f *rightsFixture) send(role, command string) (int, http.Header, map[string]any) {
	f.t.Helper()
	b, _ := json.Marshal(map[string]string{"command": command})
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, f.base+"/api/v1/servers/"+f.server+"/commands", bytes.NewReader(b))
	if err != nil {
		f.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", f.base)
	req.AddCookie(f.c[role].cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, resp.Header, body
}

func (f *rightsFixture) listEntries(entries ...map[string]any) {
	f.t.Helper()
	f.as(rSI).must(http.MethodPatch, "/command-lists/"+f.list, map[string]any{"entries": entries}, nil, http.StatusOK)
}

// TestCommandRulesOverTheAPI covers CMD-002, CMD-004 and CMD-005: the same
// API that gives admins free input checks moderators and supporters.
func TestCommandRulesOverTheAPI(t *testing.T) {
	f := newRightsFixture(t)
	f.listEntries(
		map[string]any{"path": "kick", "passable": false},
		map[string]any{"path": "say", "passable": true},
		map[string]any{"path": "execute", "passable": true},
		map[string]any{"path": "whitelist add", "passable": true},
		map[string]any{"path": "/mymod reload", "passable": true},
	)
	if status, _, b := f.send(rA, "op steve"); status != http.StatusOK {
		t.Errorf("free input of the RCON admin = %d %v", status, b)
	}
	if status, _, b := f.send(rSI, "op steve"); status != http.StatusOK {
		t.Errorf("free input of the server admin = %d %v", status, b)
	}
	// CAT-002: the catalog limits nobody with free input.
	if status, _, b := f.send(rA, "list uuids"); status != http.StatusOK {
		t.Errorf("list uuids of the RCON admin = %d %v", status, b)
	}
	for _, role := range []string{rMR, rSU} {
		for command, want := range map[string]struct {
			status int
			reason string
		}{
			"op steve":           {http.StatusForbidden, "not_in_list"},
			"execute run say hi": {http.StatusForbidden, "wrapper_blocked"},
			"whitelist add x y":  {http.StatusUnprocessableEntity, "invalid"},
		} {
			status, _, b := f.send(role, command)
			if status != want.status || b["reason"] != want.reason {
				t.Errorf("%s sends %q = %d %v, want %+v", role, command, status, b, want)
			}
		}
		if status, _, b := f.send(role, "whitelist add x"); status != http.StatusOK {
			t.Errorf("%s sends whitelist add x = %d %v", role, status, b)
		}
		// CAT-002: a command no catalog knows, listed with its prefix.
		for _, command := range []string{"mymod reload", "/mymod reload"} {
			if status, _, b := f.send(role, command); status != http.StatusOK {
				t.Errorf("%s sends %s = %d %v", role, command, status, b)
			}
		}
	}
}

// TestCommandAuditOverTheAPI covers CMD-007: every audit entry of a command,
// also of a refused one, carries request ID, session, user and role, and the
// answer carries the same request ID.
func TestCommandAuditOverTheAPI(t *testing.T) {
	f := newRightsFixture(t)
	db := f.db()
	for command, want := range map[string]int{"say hi": http.StatusOK, "op steve": http.StatusForbidden} {
		status, header, body := f.send(rMR, command)
		if status != want {
			t.Fatalf("%q = %d %v", command, status, body)
		}
		id := header.Get("X-Request-ID")
		if got, ok := body["request_id"].(string); ok && got != id {
			t.Errorf("%q: body request_id %s, header %s", command, got, id)
		}
		if id == "" {
			t.Fatalf("%q: no request ID in the answer", command)
		}
		n := f.checkAuditEntries(db, command, id)
		if n == 0 {
			t.Errorf("%q: no audit entry with request ID %s", command, id)
		}
	}
}

// checkAuditEntries wants request ID, session, user and role on all entries
// of a request; it returns how many there are.
func (f *rightsFixture) checkAuditEntries(db *sql.DB, command, id string) int {
	f.t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT action, actor_id, actor_role, COALESCE(session_id, '') FROM audit_log
		WHERE id = ? OR payload LIKE ?`, id, "%"+id+"%")
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	n := 0
	for rows.Next() {
		var action, actor, role, session string
		if err := rows.Scan(&action, &actor, &role, &session); err != nil {
			f.t.Fatal(err)
		}
		n++
		if actor != f.ids[rMR] || role != "moderator" || session == "" {
			f.t.Errorf("%q: entry %s = %s %s %q", command, action, actor, role, session)
		}
	}
	if err := rows.Err(); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// TestRateLimitOverTheAPI covers CMD-019: over the limit a moderator gets 429
// with the waiting time, and the attempt is in the audit log.
func TestRateLimitOverTheAPI(t *testing.T) {
	f := newRightsFixture(t)
	f.as(rA).must(http.MethodPatch, "/settings", map[string]any{"ratelimit.moderator.burst": 2, "ratelimit.moderator.per_minute": 20}, nil, http.StatusOK)
	var header http.Header
	for i := range 10 {
		status, h, b := f.send(rMR, "say hi")
		if status == http.StatusTooManyRequests {
			header = h
			if b["reason"] != "rate_limited" {
				t.Errorf("reason = %v", b)
			}
			break
		}
		if status != http.StatusOK {
			t.Fatalf("command %d = %d %v", i, status, b)
		}
	}
	if header == nil {
		t.Fatal("no 429 within 10 commands")
	}
	if wait, err := strconv.Atoi(header.Get("Retry-After")); err != nil || wait <= 0 {
		t.Errorf("Retry-After = %q", header.Get("Retry-After"))
	}
	var n int
	if err := f.db().QueryRowContext(context.Background(), `SELECT count(*) FROM audit_log WHERE reason_code = 'rate_limited' AND actor_id = ?`, f.ids[rMR]).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Error("the refused attempt is not in the audit log")
	}
}

// liveSession signs a fresh user in and opens a socket for it.
func (f *rightsFixture) liveSession(name string) (id string, c *client, closed <-chan error) {
	f.t.Helper()
	id = f.invite(rA, "server_admin", name)
	c = login(f.t, f.base, name, matrixPassword)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(f.base, "http")+"/api/v1/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {f.base}, "Cookie": {c.cookie.String()}},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = ws.CloseNow() })
	done := make(chan error, 1)
	go func() {
		for {
			if _, _, err := ws.Read(context.Background()); err != nil {
				done <- err
				return
			}
		}
	}()
	return id, c, done
}

func (f *rightsFixture) wantEnded(what string, c *client, closed <-chan error) {
	f.t.Helper()
	if status := c.call(http.MethodGet, "/auth/session", nil, nil); status != http.StatusUnauthorized {
		f.t.Errorf("%s: old session = %d", what, status)
	}
	select {
	case err := <-closed:
		f.t.Logf("%s: socket ended: %v", what, err)
	case <-time.After(5 * time.Second):
		f.t.Errorf("%s: the WebSocket stayed open", what)
	}
}

// TestLockAndResetEndSessions covers AUTH-006 for a lock and AUTH-010 for a
// reset, with a real session and socket.
func TestLockAndResetEndSessions(t *testing.T) {
	f := newRightsFixture(t)
	id, c, closed := f.liveSession("locked-user")
	f.as(rA).must(http.MethodPatch, "/users/"+id, map[string]bool{"disabled": true}, nil, http.StatusOK)
	f.wantEnded("lock", c, closed)

	id, c, closed = f.liveSession("reset-user")
	f.as(rA).must(http.MethodPost, "/users/"+id+"/password-reset", nil, nil, http.StatusOK)
	f.wantEnded("reset", c, closed)
}

// TestRoleLimitsOverTheAPI covers ROLE-006 and ROLE-008: nobody creates,
// changes or demotes an equal or higher role, and supporters need both
// switches.
func TestRoleLimitsOverTheAPI(t *testing.T) {
	f := newRightsFixture(t)
	for _, c := range []struct{ by, role string }{
		{rSI, "server_admin"}, {rSI, "rcon_admin"}, {rMR, "moderator"}, {rMR, "server_admin"}, {rMR, "rcon_admin"},
	} {
		if status, _ := f.raw(c.by, http.MethodPost, "/invites", map[string]string{"role": c.role}); status < 400 {
			t.Errorf("%s invites a %s = %d", c.by, c.role, status)
		}
	}
	for _, c := range []struct{ by, target, role string }{
		{rSI, rSO, "moderator"}, {rSI, rSI, "rcon_admin"}, {rMR, rMN, "supporter"}, {rMR, rSI, "supporter"}, {rSU, rSU, "rcon_admin"},
	} {
		if status, _ := f.raw(c.by, http.MethodPatch, "/users/"+f.ids[c.target], map[string]string{"role": c.role}); status < 400 {
			t.Errorf("%s sets %s to %s = %d", c.by, c.target, c.role, status)
		}
	}

	// Own release on, global switch off.
	f.as(rA).must(http.MethodPatch, "/settings", map[string]any{"supporters.enabled": false}, nil, http.StatusOK)
	if status, _ := f.raw(rMR, http.MethodPost, "/invites", map[string]string{"role": "supporter"}); status < 400 {
		t.Errorf("invitation with the global switch off = %d", status)
	}
	grant := map[string]any{"supporter_grants": []map[string]string{{"moderator_id": f.ids[rMR], "server_id": f.server}}}
	if status, _ := f.raw(rMR, http.MethodPut, "/users/"+f.ids[rSU]+"/assignments", grant); status < 400 {
		t.Errorf("assignment with the global switch off = %d", status)
	}
	// Global switch on, own release off.
	f.as(rA).must(http.MethodPatch, "/settings", map[string]any{"supporters.enabled": true}, nil, http.StatusOK)
	if status, _ := f.raw(rMN, http.MethodPost, "/invites", map[string]string{"role": "supporter"}); status < 400 {
		t.Errorf("invitation without own release = %d", status)
	}
}

// TestChangesAreAudited covers the administration part of AUD-001: each
// change leaves an entry with its result.
func TestChangesAreAudited(t *testing.T) {
	f := newRightsFixture(t)
	admin := f.as(rA)
	var srv, sg, grp, list, inv struct{ ID string }
	admin.must(http.MethodPost, "/servers", map[string]any{"name": "audited", "profile_id": f.profile(), "host": "127.0.0.1", "port": 1, "password": "x"}, &srv, http.StatusCreated)
	admin.must(http.MethodPatch, "/servers/"+srv.ID, map[string]string{"name": "audited2"}, nil, http.StatusOK)
	admin.must(http.MethodPost, "/server-groups", map[string]any{"name": "audited", "server_ids": []string{}}, &sg, http.StatusCreated)
	admin.must(http.MethodPatch, "/server-groups/"+sg.ID, map[string]string{"name": "audited2"}, nil, http.StatusOK)
	admin.must(http.MethodPost, "/command-lists", map[string]any{"name": "audited", "entries": []map[string]any{{"path": "say", "passable": false}, {"path": "list", "passable": true}}}, &list, http.StatusCreated)
	admin.must(http.MethodPatch, "/command-lists/"+list.ID, map[string]string{"name": "audited2"}, nil, http.StatusOK)
	admin.must(http.MethodPost, "/user-groups", map[string]any{"name": "audited", "kind": "general"}, &grp, http.StatusCreated)
	admin.must(http.MethodPatch, "/user-groups/"+grp.ID, map[string]string{"name": "audited2"}, nil, http.StatusOK)
	admin.must(http.MethodPost, "/invites", map[string]string{"role": "supporter"}, nil, http.StatusCreated)
	var invites struct{ Items []struct{ ID string } }
	admin.must(http.MethodGet, "/invites", nil, &invites, http.StatusOK)
	inv.ID = invites.Items[0].ID
	admin.must(http.MethodDelete, "/invites/"+inv.ID, nil, nil, http.StatusNoContent)
	admin.must(http.MethodPost, "/agents/tokens", nil, nil, http.StatusCreated)
	id := f.invite(rA, "supporter", "audited")
	login(t, f.base, "audited", matrixPassword).must(http.MethodPost, "/me/password", map[string]string{"current_password": matrixPassword, "new_password": "another matrix password"}, nil, http.StatusNoContent)
	admin.must(http.MethodDelete, "/users/"+id, nil, nil, http.StatusNoContent)
	admin.must(http.MethodDelete, "/servers/"+srv.ID, nil, nil, http.StatusNoContent)
	admin.must(http.MethodDelete, "/server-groups/"+sg.ID, nil, nil, http.StatusNoContent)
	admin.must(http.MethodDelete, "/user-groups/"+grp.ID, nil, nil, http.StatusNoContent)
	admin.must(http.MethodDelete, "/command-lists/"+list.ID, nil, nil, http.StatusNoContent)

	db := f.db()
	for _, action := range []string{
		"server.create", "server.update", "server.delete", "server_group.create", "server_group.update", "server_group.delete",
		"command_list.create", "command_list.update", "command_list.delete", "group.create", "group.update", "group.delete",
		"invite.create", "invite.delete", "invite.redeem", "agent.token.create", "password.change", "user.delete",
	} {
		var n int
		if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM audit_log WHERE action = ? AND result = 'ok'`, action).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Errorf("no audit entry with result for %s", action)
		}
	}
}

// restartable is a panel with one server that a test stops and starts.
type restartable struct {
	t      *testing.T
	root   string
	base   string
	stop   func() error
	logs   *syncBuffer
	admin  *client
	server string
}

func (r *restartable) start() string {
	r.t.Helper()
	var console syncBuffer
	r.logs = &syncBuffer{}
	addr, stop := startPanel(r.t, testConfig(r.t, r.root), r.logs, &console)
	r.stop, r.base = stop, "http://"+addr
	return console.String()
}

func newRestartable(t *testing.T, game *rcontest.Server, rconPassword string) *restartable {
	t.Helper()
	r := &restartable{t: t, root: t.TempDir()}
	m := regexp.MustCompile(`password: (\S+)`).FindStringSubmatch(r.start())
	t.Cleanup(func() {
		if r.stop != nil {
			_ = r.stop()
		}
	})
	r.admin = login(t, r.base, "admin", m[1])
	r.admin.must(http.MethodPost, "/me/password", map[string]string{"current_password": m[1], "new_password": matrixPassword}, nil, http.StatusNoContent)
	var p struct{ Items []struct{ ID, Slug string } }
	r.admin.must(http.MethodGet, "/profiles", nil, &p, http.StatusOK)
	host, port, _ := net.SplitHostPort(game.Addr)
	n, _ := strconv.Atoi(port)
	var s struct{ ID string }
	r.admin.must(http.MethodPost, "/servers", map[string]any{"name": "mc", "profile_id": p.Items[0].ID, "host": host, "port": n, "password": rconPassword}, &s, http.StatusCreated)
	r.server = s.ID
	return r
}

// waitServer waits for a status over the API and returns how long it took.
func (r *restartable) waitServer(want string) time.Duration {
	r.t.Helper()
	start := time.Now()
	for {
		var s struct{ Status string }
		r.admin.must(http.MethodGet, "/servers/"+r.server, nil, &s, http.StatusOK)
		if s.Status == want {
			return time.Since(start)
		}
		if time.Since(start) > 20*time.Second {
			r.t.Fatalf("status %s, want %s", s.Status, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestDeletedMasterKey covers SRV-002: the password stands only encrypted in
// the database; without the master key the panel starts and asks for the
// credentials again.
func TestDeletedMasterKey(t *testing.T) {
	const password = "the rcon password in question"
	game, err := rcontest.Start(password, rcontest.Minecraft, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(game.Close)
	r := newRestartable(t, game, password)
	r.waitServer("connected")
	if err := r.stop(); err != nil {
		t.Fatal(err)
	}
	r.stop = nil
	for _, name := range []string{"rconductor.db", "rconductor.db-wal"} {
		b, err := os.ReadFile(filepath.Join(r.root, "data", name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte(password)) {
			t.Errorf("the password stands readable in %s", name)
		}
	}
	if err := os.Remove(filepath.Join(r.root, "secrets", "master.key")); err != nil {
		t.Fatal(err)
	}
	r.start()
	r.admin = login(t, r.base, "admin", matrixPassword)
	logtest.Wait(t, r.logs, "enter them again")
	var s struct{ Credentials string }
	r.admin.must(http.MethodGet, "/servers/"+r.server, nil, &s, http.StatusOK)
	if s.Credentials != "reenter" {
		t.Errorf("credentials = %q, want reenter", s.Credentials)
	}
}

// TestPlayersAfterRestart covers SRV-006: runtime data is in no table, and
// after a restart the panel asks the game server again.
func TestPlayersAfterRestart(t *testing.T) {
	asked := make(chan struct{}, 100)
	game, err := rcontest.Start("rconpw", rcontest.Minecraft, func(cmd string) string {
		if cmd == "list" {
			asked <- struct{}{}
			return "There are 1 of a max of 20 players online: Steve"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(game.Close)
	r := newRestartable(t, game, "rconpw")
	r.waitServer("connected")
	players := func() {
		t.Helper()
		var p struct{ Players []string }
		r.admin.must(http.MethodGet, "/servers/"+r.server+"/players", nil, &p, http.StatusOK)
		if len(p.Players) != 1 || p.Players[0] != "Steve" {
			t.Errorf("players = %+v", p)
		}
	}
	players()
	if err := r.stop(); err != nil {
		t.Fatal(err)
	}
	r.stop = nil
	db, err := sql.Open("sqlite", filepath.Join(r.root, "data", "rconductor.db"))
	if err != nil {
		t.Fatal(err)
	}
	var tables int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND
		(sql LIKE '%cpu%' OR sql LIKE '%mem_bytes%' OR sql LIKE '%player_count%' OR name LIKE '%player%' OR name LIKE '%metric%')`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if tables != 0 {
		t.Errorf("%d tables hold runtime data", tables)
	}
	for len(asked) > 0 {
		<-asked
	}
	r.start()
	r.admin = login(t, r.base, "admin", matrixPassword)
	r.waitServer("connected")
	players()
	if len(asked) == 0 {
		t.Error("the panel did not ask the game server after the restart")
	}
}

// TestStatusChangesWithin10Seconds covers SRV-004 with the default timing,
// read over the API: wrong password, stopped game, game back.
func TestStatusChangesWithin10Seconds(t *testing.T) {
	game, err := rcontest.Start("rconpw", rcontest.Minecraft, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := newRestartable(t, game, "rconpw")
	r.waitServer("connected")
	addr := game.Addr
	game.Close()
	if d := r.waitServer("offline"); d > 10*time.Second {
		t.Errorf("offline after %v", d)
	}
	back, err := rcontest.StartOn(addr, "rconpw", rcontest.Minecraft, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d := r.waitServer("connected"); d > 10*time.Second {
		t.Errorf("connected after %v", d)
	}
	back.Close()
	r.waitServer("offline")
	wrong, err := rcontest.StartOn(addr, "another password", rcontest.Minecraft, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(wrong.Close)
	if d := r.waitServer("auth_failed"); d > 10*time.Second {
		t.Errorf("auth_failed after %v", d)
	}
}

// TestProfileFileInARunningPanel covers CAT-003: a changed file in
// /data/profiles shows over the API within 5 s, a broken change keeps the
// last state.
func TestProfileFileInARunningPanel(t *testing.T) {
	game, err := rcontest.Start("rconpw", rcontest.Minecraft, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(game.Close)
	r := newRestartable(t, game, "rconpw")
	shipped, err := os.ReadFile("../../profiles/minecraft-java.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(r.root, "data", "profiles")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "custom.yaml")
	profile := func(name string) []byte {
		b := regexp.MustCompile(`(?m)^id: .*$`).ReplaceAll(shipped, []byte("id: custom-game"))
		return regexp.MustCompile(`(?m)^name: .*$`).ReplaceAll(b, []byte("name: "+name))
	}
	nameOf := func() string {
		var p struct{ Items []struct{ Slug, Name string } }
		r.admin.must(http.MethodGet, "/profiles", nil, &p, http.StatusOK)
		for _, x := range p.Items {
			if x.Slug == "custom-game" {
				return x.Name
			}
		}
		return ""
	}
	waitName := func(want string) {
		t.Helper()
		start := time.Now()
		for nameOf() != want {
			if time.Since(start) > 5*time.Second {
				t.Fatalf("profile name %q, want %q within 5 s", nameOf(), want)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	if err := os.WriteFile(file, profile("First name"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitName("First name")
	if err := os.WriteFile(file, profile("Second name"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitName("Second name")
	if err := os.WriteFile(file, []byte("schema: [broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	logtest.Wait(t, r.logs, `"event":"profile.rejected"`)
	if got := nameOf(); got != "Second name" {
		t.Errorf("after a broken change the name is %q", got)
	}
}

// TestBigBodies covers T-025: without a session nobody fills the memory with
// a body; the largest legal body, a profile picture, still passes the limit.
func TestBigBodies(t *testing.T) {
	f := newRightsFixture(t)
	post := func(body io.Reader, length int64) int {
		t.Helper()
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, f.base+"/api/v1/auth/login", body)
		if err != nil {
			t.Fatal(err)
		}
		req.ContentLength = length
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", f.base)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode
	}
	huge := append(append([]byte(`{"pseudonym":"admin","password":"`), bytes.Repeat([]byte("x"), 3<<20)...), []byte(`"}`)...)
	if status := post(bytes.NewReader(huge), int64(len(huge))); status != http.StatusRequestEntityTooLarge {
		t.Errorf("declared length = %d", status)
	}
	// Without a declared length the reader stops at the limit.
	if status := post(io.MultiReader(bytes.NewReader(huge)), -1); status != http.StatusBadRequest && status != http.StatusRequestEntityTooLarge {
		t.Errorf("unknown length = %d", status)
	}
	picture := bytes.Repeat([]byte("x"), 1<<20)
	if status, _ := f.raw(rA, http.MethodPut, "/me/avatar", picture); status == http.StatusRequestEntityTooLarge {
		t.Errorf("a body of 1 MiB is refused as too large")
	}
}

// TestStoredPasswordStaysAtItsHost covers T-011: changing host or port needs
// the password again, or a stolen session could read it at a listener.
func TestStoredPasswordStaysAtItsHost(t *testing.T) {
	f := newRightsFixture(t)
	if status, reason := f.raw(rA, http.MethodPatch, "/servers/"+f.server, map[string]string{"host": "attacker.example"}); status != http.StatusUnprocessableEntity && status != http.StatusBadRequest {
		t.Errorf("new host without password = %d %s", status, reason)
	}
	if status, _ := f.raw(rA, http.MethodPatch, "/servers/"+f.server, map[string]any{"port": 1}); status < 400 {
		t.Errorf("new port without password = %d", status)
	}
	if status, _ := f.raw(rA, http.MethodPatch, "/servers/"+f.server, map[string]string{"name": "renamed"}); status != http.StatusOK {
		t.Errorf("another field = %d", status)
	}
	if status, _ := f.raw(rA, http.MethodPatch, "/servers/"+f.server, map[string]string{"host": "mc.example", "password": "a new password"}); status != http.StatusOK {
		t.Errorf("new host with password = %d", status)
	}
}

// TestAuditRetentionFloor covers T-009: a stolen session cannot erase the
// audit log by a short retention.
func TestAuditRetentionFloor(t *testing.T) {
	f := newRightsFixture(t)
	if status, _ := f.raw(rA, http.MethodPatch, "/settings", map[string]int{"audit.retention_days": 29}); status < 400 {
		t.Errorf("29 days = %d", status)
	}
	f.as(rA).must(http.MethodPatch, "/settings", map[string]int{"audit.retention_days": 30}, nil, http.StatusOK)
}
