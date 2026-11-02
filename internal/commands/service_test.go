// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/connmgr"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/rcon"
	"github.com/NightDragon1/rconductor/internal/rcon/rcontest"
	"github.com/NightDragon1/rconductor/internal/rcon/source"
	"github.com/NightDragon1/rconductor/internal/servers"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/secret"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
)

type lockedLog struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// world: one Minecraft test server s1; admin; server admin sa with scope
// s1; moderator m in sa's group mg with list l (whitelist add passable,
// kick, execute); supporter sup of m on s1.
type world struct {
	t        *testing.T
	st       *store.Store
	svc      *Service
	users    *users.Service
	c        *perm.Checker
	logs     *lockedLog
	ids      map[string]string
	server   string
	received []string
	mu       sync.Mutex
	game     *rcontest.Server
}

func newWorld(t *testing.T) *world {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	st := storetest.New(t)
	logs := &lockedLog{}
	log := logging.New(logs, "panel", config.Log{Format: "json"}).Module("cmd")
	w := &world{t: t, st: st, logs: logs, ids: map[string]string{}}
	for _, f := range []func(context.Context, *store.Store) error{perm.EnsureDefaults, EnsureDefaults} {
		if err := f(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	if err := profiles.NewImporter(st, log, os.DirFS("../../profiles"), t.TempDir()).Sync(ctx); err != nil {
		t.Fatal(err)
	}
	game, err := rcontest.Start("rconpw", rcontest.Minecraft, func(cmd string) string {
		w.mu.Lock()
		w.received = append(w.received, cmd)
		w.mu.Unlock()
		switch cmd {
		case "big":
			return strings.Repeat("x", MaxStoredResponse+100)
		case "slow 1", "slow 2":
			time.Sleep(250 * time.Millisecond)
		case "html":
			return "<b>hi</b><script>alert(1)</script>\x1b[31m\r\nline\x00 2 \u00a7c\xff"
		}
		return "ok: " + cmd
	})
	if err != nil {
		t.Fatal(err)
	}
	w.game = game
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	box, _ := secret.New(key)
	w.c = perm.NewChecker(st, log)
	srvSvc := servers.NewService(st, w.c, box, log)
	conns := connmgr.New(rcon.Registry{"source-rcon": source.Adapter{}}, srvSvc, log, connmgr.Options{ProbeInterval: 50 * time.Millisecond, MaxBackoff: 100 * time.Millisecond})
	srvSvc.SetConnections(conns)
	if err := conns.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); conns.Wait(); game.Close() })
	w.svc = NewService(st, w.c, conns, log)
	w.users = users.NewService(st, w.c, log, nil)

	add := func(name, role, by string) {
		w.ids[name] = storetest.CreateUser(t, st, storetest.User{Pseudonym: name, Role: role, CreatedBy: w.ids[by]})
	}
	add("admin", "rcon_admin", "")
	add("sa", "server_admin", "admin")
	add("m", "moderator", "sa")
	add("sup", "supporter", "m")

	var profile string
	_ = st.DB.QueryRowContext(ctx, `SELECT id FROM game_profiles WHERE slug = 'minecraft-java'`).Scan(&profile)
	host, port, _ := net.SplitHostPort(game.Addr)
	p, _ := strconv.Atoi(port)
	name, pw := "mc", logging.Secret("rconpw")
	s, err := srvSvc.Create(ctx, w.as("admin"), servers.Input{Name: &name, ProfileID: &profile, Host: &host, Port: &p, Password: &pw})
	if err != nil {
		t.Fatal(err)
	}
	w.server = s.ID
	w.exec(`INSERT INTO admin_scopes (id, user_id, server_id, assigned_by, created_at) VALUES ('as', ?, ?, ?, 't')`, w.ids["sa"], s.ID, w.ids["admin"])
	w.exec(`INSERT INTO command_lists (id, name, owner_id, created_at, updated_at) VALUES ('l', 'l', ?, 't', 't')`, w.ids["sa"])
	for i, e := range []struct {
		path     string
		passable int
	}{{"whitelist add", 1}, {"kick", 0}, {"execute", 0}} {
		w.exec(`INSERT INTO command_list_entries (id, command_list_id, path, passable) VALUES (?, 'l', ?, ?)`, "e"+strconv.Itoa(i), e.path, e.passable)
	}
	w.exec(`INSERT INTO user_groups (id, name, kind, owner_id, command_list_id, created_at, updated_at) VALUES ('mg', 'mg', 'moderator', ?, 'l', 't', 't')`, w.ids["sa"])
	w.exec(`INSERT INTO user_group_members (user_group_id, user_id, added_by, created_at) VALUES ('mg', ?, ?, 't')`, w.ids["m"], w.ids["sa"])
	w.exec(`INSERT INTO grants (id, user_group_id, server_id, granted_by, created_at) VALUES ('g', 'mg', ?, ?, 't')`, s.ID, w.ids["sa"])
	w.exec(`UPDATE settings SET value = 'true' WHERE key = 'supporters.enabled'`)
	w.exec(`UPDATE users SET supporters_allowed = 1 WHERE id = ?`, w.ids["m"])
	w.exec(`INSERT INTO supporter_grants (id, supporter_id, moderator_id, server_id, granted_by, created_at) VALUES ('sg', ?, ?, ?, ?, 't')`, w.ids["sup"], w.ids["m"], s.ID, w.ids["m"])
	w.c.Invalidate()

	deadline := time.Now().Add(10 * time.Second)
	for {
		if st, _ := conns.Status(s.ID); st == connmgr.Connected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("test server not connected")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return w
}

func (w *world) exec(q string, args ...any) {
	w.t.Helper()
	if _, err := w.st.DB.ExecContext(context.Background(), q, args...); err != nil {
		w.t.Fatalf("%s: %v", q, err)
	}
}

func (w *world) as(name string) users.Request {
	w.t.Helper()
	r, err := w.c.For(context.Background(), w.ids[name])
	if err != nil {
		w.t.Fatal(err)
	}
	id, _ := store.NewID()
	return users.Request{
		Actor: perm.Actor{ID: w.ids[name], Role: r.Role}, Pseudonym: name,
		SessionRef: "abcd1234", SessionID: "abcd1234session-" + name, RequestID: id,
	}
}

func (w *world) send(name, cmd string) (Result, error) {
	w.t.Helper()
	return w.svc.Send(context.Background(), w.as(name), w.server, cmd)
}

func reason(err error) string {
	var d *DeniedError
	if errors.As(err, &d) {
		return d.Reason
	}
	return ""
}

func (w *world) gameReceived() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.received...)
}

// TestFreeInputOnlyForAdmins covers CMD-002 and ROLE-005.
func TestFreeInputOnlyForAdmins(t *testing.T) {
	w := newWorld(t)
	for _, admin := range []string{"admin", "sa"} {
		if r, err := w.send(admin, "op steve"); err != nil || r.Response != "ok: op steve" {
			t.Errorf("%s free input: %+v %v", admin, r, err)
		}
	}
	if r, err := w.send("m", "whitelist add steve"); err != nil || r.Response != "ok: whitelist add steve" {
		t.Errorf("moderator list command: %+v %v", r, err)
	}
	if _, err := w.send("m", "op steve"); reason(err) != ReasonNotInList {
		t.Errorf("moderator free input: %v", err)
	}
}

// TestWrapperRefused covers CMD-005: execute … run say hi is refused for
// moderators although execute is in their list.
func TestWrapperRefused(t *testing.T) {
	w := newWorld(t)
	if _, err := w.send("m", "execute as @a run say hi"); reason(err) != ReasonWrapperBlocked {
		t.Errorf("wrapper: %v", err)
	}
	if !strings.Contains(w.logs.String(), `"reason":"wrapper_blocked"`) {
		t.Error("cmd.denied with wrapper_blocked missing")
	}
	if _, err := w.send("admin", "execute as @a run say hi"); err != nil {
		t.Errorf("admin wrapper: %v", err)
	}
}

// TestSupporterPassableOnly covers the sending part of ROLE-007.
func TestSupporterPassableOnly(t *testing.T) {
	w := newWorld(t)
	if _, err := w.send("sup", "whitelist add alex"); err != nil {
		t.Errorf("passable command: %v", err)
	}
	if _, err := w.send("sup", "kick alex"); reason(err) != ReasonNotInList {
		t.Errorf("locked command: %v", err)
	}
}

// TestCleaningAtTheServer covers CMD-006 end to end: say a\nop x runs as
// one single command.
func TestCleaningAtTheServer(t *testing.T) {
	w := newWorld(t)
	before := len(w.gameReceived())
	if _, err := w.send("admin", "say a\nop x"); err != nil {
		t.Fatal(err)
	}
	got := w.gameReceived()[before:]
	if !slices.Equal(got, []string{"say aop x"}) {
		t.Errorf("game server received %q", got)
	}
}

// TestProfileSyntax covers CAT-005 in the pipeline and CAT-002.
func TestProfileSyntax(t *testing.T) {
	w := newWorld(t)
	if r, err := w.send("m", "/KICK steve"); err != nil || r.Command != "KICK steve" {
		t.Errorf("/KICK with kick in the list: %+v %v", r, err)
	}
	if _, err := w.send("admin", strings.Repeat("a", 1001)); reason(err) != ReasonInvalid {
		t.Errorf("too long: %v", err)
	}
	// CAT-002: a command no catalog knows can be listed and sent.
	w.exec(`INSERT INTO command_list_entries (id, command_list_id, path, passable) VALUES ('e9', 'l', 'mymod reload', 0)`)
	w.c.Invalidate()
	if _, err := w.send("m", "mymod reload all"); err != nil {
		t.Errorf("command without catalog entry: %v", err)
	}
}

// TestMarking covers CMD-007: audit entry and log lines carry request ID,
// session, user and role; the answer carries the same request ID.
func TestMarking(t *testing.T) {
	w := newWorld(t)
	req := w.as("m")
	r, err := w.svc.Send(context.Background(), req, w.server, "whitelist add steve")
	if err != nil {
		t.Fatal(err)
	}
	if r.RequestID != req.RequestID {
		t.Errorf("answer request ID %s, want %s", r.RequestID, req.RequestID)
	}
	var kind, actor, role, session, server, result string
	if err := w.st.DB.QueryRowContext(context.Background(),
		`SELECT kind, actor_id, actor_role, session_id, server_id, result FROM audit_log WHERE id = ?`, req.RequestID,
	).Scan(&kind, &actor, &role, &session, &server, &result); err != nil {
		t.Fatal(err)
	}
	if kind != "command" || actor != w.ids["m"] || role != "moderator" || session != req.SessionID || server != w.server || result != "ok" {
		t.Errorf("audit = %s %s %s %s %s %s", kind, actor, role, session, server, result)
	}
	// The result is a second, appended entry that refers to the request ID.
	var resActor, resSession, resResult, resPayload string
	if err := w.st.DB.QueryRowContext(context.Background(),
		`SELECT actor_id, session_id, result, payload FROM audit_log WHERE action = 'cmd.result' AND target_type = 'command' AND target_id = ?`, req.RequestID,
	).Scan(&resActor, &resSession, &resResult, &resPayload); err != nil {
		t.Fatalf("cmd.result entry: %v", err)
	}
	if resActor != w.ids["m"] || resSession != req.SessionID || resResult != "ok" ||
		!strings.Contains(resPayload, `"request_id":"`+req.RequestID+`"`) || !strings.Contains(resPayload, `"duration_ms":`) {
		t.Errorf("cmd.result = %s %s %s %s", resActor, resSession, resResult, resPayload)
	}
	for _, ev := range []string{"cmd.accepted", "cmd.sent", "cmd.response"} {
		found := false
		for line := range strings.Lines(w.logs.String()) {
			if strings.Contains(line, `"event":"`+ev+`"`) && strings.Contains(line, `"request_id":"`+req.RequestID+`"`) &&
				strings.Contains(line, `"session":"abcd1234"`) && strings.Contains(line, `"user":"m"`) && strings.Contains(line, `"role":"moderator"`) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s without request ID, session, user and role", ev)
		}
	}
}

// TestAnswerIsForeignData covers the command part of SEC-006: HTML stays
// text, control characters and invalid UTF-8 go, color codes stay for the
// interface, in the answer, the history and the audit alike.
func TestAnswerIsForeignData(t *testing.T) {
	w := newWorld(t)
	r, err := w.svc.Send(context.Background(), w.as("admin"), w.server, "html")
	if err != nil {
		t.Fatal(err)
	}
	want := "<b>hi</b><script>alert(1)</script>[31m\nline 2 \u00a7c\uFFFD"
	if r.Response != want || r.Truncated {
		t.Errorf("response = %q, %v; want %q", r.Response, r.Truncated, want)
	}
	h, _, err := w.svc.History(context.Background(), w.as("admin"), w.server, 10, "")
	if err != nil || len(h) == 0 || h[0].Response != want {
		t.Errorf("history = %+v, %v", h, err)
	}
}

// TestRateLimit covers CMD-019: a moderator over the limit of Design 3.4
// gets rate_limited with a wait, and the attempt is audited.
func TestRateLimitInPipeline(t *testing.T) {
	w := newWorld(t)
	var last error
	for range 6 {
		_, last = w.send("m", "whitelist add x")
	}
	var d *DeniedError
	if !errors.As(last, &d) || d.Reason != ReasonRateLimited || d.RetryAfter <= 0 {
		t.Fatalf("sixth command: %v", last)
	}
	var n int
	_ = w.st.DB.QueryRowContext(context.Background(), `SELECT count(*) FROM audit_log WHERE kind = 'command' AND reason_code = 'rate_limited'`).Scan(&n)
	if n != 1 {
		t.Errorf("audited rate limits = %d", n)
	}
}

// TestListChangeWithoutNewSession is the M2 test case of issue #43: sending
// checks the current list; a change works without a new session.
func TestListChangeWithoutNewSession(t *testing.T) {
	w := newWorld(t)
	sa := w.as("sa")
	entries := []users.Entry{{Path: "say"}, {Path: "list", Passable: true}}
	if _, err := w.users.UpdateCommandList(context.Background(), sa, "l", nil, &entries); err != nil {
		t.Fatal(err)
	}
	if _, err := w.send("m", "whitelist add steve"); reason(err) != ReasonNotInList {
		t.Errorf("removed command still allowed: %v", err)
	}
	if _, err := w.send("m", "say hello"); err != nil {
		t.Errorf("new command refused: %v", err)
	}
}

func TestScopeAndUnavailable(t *testing.T) {
	w := newWorld(t)
	if _, err := w.svc.Send(context.Background(), w.as("m"), "other-server", "list"); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("server outside the scope: %v", err)
	}
	r, err := w.send("admin", "big")
	if err != nil || !r.Truncated || len(r.Response) != MaxStoredResponse {
		t.Errorf("big answer: truncated=%v len=%d %v", r.Truncated, len(r.Response), err)
	}
	w.game.Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, err = w.send("admin", "list")
		if errors.Is(err, ErrUnavailable) || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("stopped game server: %v", err)
	}
}

func TestHistory(t *testing.T) {
	w := newWorld(t)
	_, _ = w.send("admin", "op steve")
	_, _ = w.send("m", "whitelist add steve")
	_, _ = w.send("m", "op steve") // refused, also in the history
	all, _, err := w.svc.History(context.Background(), w.as("admin"), w.server, 50, "")
	if err != nil || len(all) != 3 {
		t.Fatalf("admin history: %d %v", len(all), err)
	}
	if all[0].Result != ResultDenied || all[0].Reason != ReasonNotInList || all[0].Pseudonym != "m" {
		t.Errorf("newest entry = %+v", all[0])
	}
	own, _, _ := w.svc.History(context.Background(), w.as("m"), w.server, 50, "")
	for _, e := range own {
		if e.UserID != w.ids["m"] {
			t.Errorf("moderator sees a command of %s", e.Pseudonym)
		}
	}
	if len(own) != 2 {
		t.Errorf("moderator history = %d entries", len(own))
	}
	page, next, _ := w.svc.History(context.Background(), w.as("admin"), w.server, 2, "")
	rest, _, _ := w.svc.History(context.Background(), w.as("admin"), w.server, 2, next)
	if len(page) != 2 || len(rest) != 1 || next == "" {
		t.Errorf("paging: %d + %d", len(page), len(rest))
	}
}

// TestAuditAppendOnly: sending a command only adds entries; the entry
// written before sending is never changed.
func TestAuditAppendOnly(t *testing.T) {
	w := newWorld(t)
	req := w.as("admin")
	if _, err := w.svc.Send(context.Background(), req, w.server, "list"); err != nil {
		t.Fatal(err)
	}
	var sentResult, sentPayload string
	_ = w.st.DB.QueryRowContext(context.Background(), `SELECT result, payload FROM audit_log WHERE id = ?`, req.RequestID).Scan(&sentResult, &sentPayload)
	if sentResult != "ok" || sentPayload != `{"command":"list"}` {
		t.Errorf("cmd.sent entry was changed: %s %s", sentResult, sentPayload)
	}
	var n int
	_ = w.st.DB.QueryRowContext(context.Background(), `SELECT count(*) FROM audit_log WHERE kind = 'command' AND (id = ? OR target_id = ?)`, req.RequestID, req.RequestID).Scan(&n)
	if n != 2 {
		t.Errorf("entries for one command = %d, want cmd.sent and cmd.result", n)
	}
}

// TestTimeoutIncludesQueue covers Design 3.8: the time limit runs from the
// acceptance, the wait in the queue included; a command whose time ran out
// while waiting is never sent.
func TestTimeoutIncludesQueue(t *testing.T) {
	w := newWorld(t)
	w.svc.timeout = 100 * time.Millisecond
	// A command with more time holds the connection for 250 ms.
	blocker := NewService(w.st, w.c, w.svc.exec, w.svc.log)
	done := make(chan error, 1)
	go func() { _, err := blocker.Send(context.Background(), w.as("admin"), w.server, "slow 1"); done <- err }()
	for !slices.Contains(w.gameReceived(), "slow 1") {
		time.Sleep(5 * time.Millisecond)
	}
	start := time.Now()
	if _, err := w.send("admin", "late"); !errors.Is(err, ErrTimeout) {
		t.Errorf("queued command: %v, want timeout", err)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Errorf("timeout after %v, want 100 ms from the acceptance", d)
	}
	if err := <-done; err != nil {
		t.Fatalf("blocker: %v", err)
	}
	if _, err := w.send("admin", "next"); err != nil {
		t.Errorf("after the timeout: %v", err)
	}
	if slices.Contains(w.gameReceived(), "late") {
		t.Error("a command whose time ran out in the queue reached the game server")
	}
}

type fullQueue struct{}

func (fullQueue) Execute(context.Context, string, string) (string, error) {
	return "", connmgr.ErrQueueFull
}

// TestQueueFullIsBusy: a full queue answers busy at once (Design 3.5) and
// the audit records the error.
func TestQueueFullIsBusy(t *testing.T) {
	w := newWorld(t)
	svc := NewService(w.st, w.c, fullQueue{}, w.svc.log)
	req := w.as("admin")
	if _, err := svc.Send(context.Background(), req, w.server, "list"); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	var result, payload string
	if err := w.st.DB.QueryRowContext(context.Background(),
		`SELECT result, reason_code FROM audit_log WHERE action = 'cmd.result' AND target_id = ?`, req.RequestID,
	).Scan(&result, &payload); err != nil || result != "error" || payload != "busy" {
		t.Errorf("cmd.result = %s %s %v", result, payload, err)
	}
}
