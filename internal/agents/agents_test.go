// SPDX-License-Identifier: AGPL-3.0-or-later

package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/NightDragon1/rconductor/internal/agenthub"
	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/buildinfo"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/ops/keys"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
	"github.com/NightDragon1/rconductor/internal/users"
)

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type fixture struct {
	t     *testing.T
	st    *store.Store
	svc   *Service
	hub   *agenthub.Hub
	addr  string
	logs  *lockedBuf
	admin users.Request
	sa    users.Request
	clock time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st := storetest.New(t)
	ctx := context.Background()
	if err := users.EnsureAuditDefaults(ctx, st); err != nil {
		t.Fatal(err)
	}
	logs := &lockedBuf{}
	log := logging.New(logs, "panel", config.Log{Format: "json"}).Module("agent")
	panel, err := keys.LoadOrCreateIdentity(log, filepath.Join(t.TempDir(), keys.PanelKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, st: st, logs: logs, clock: time.Now()}
	f.svc = NewService(st, log, "panel.example:8443")
	f.svc.now = func() time.Time { return f.clock }
	f.hub = agenthub.New(panel, log, agenthub.Options{Registry: f.svc, Throttle: agenthub.ThrottleOptions{PerMinute: 1000, Burst: 1000}})
	f.svc.SetHub(f.hub)
	hctx, cancel := context.WithCancel(ctx)
	ready := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() { done <- f.hub.Serve(hctx, "127.0.0.1:0", func(a net.Addr) { ready <- a }) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	f.addr = (<-ready).String()
	adminID := storetest.CreateUser(t, st, storetest.User{Pseudonym: "admin", Role: "rcon_admin"})
	saID := storetest.CreateUser(t, st, storetest.User{Pseudonym: "sa", Role: "server_admin", CreatedBy: adminID})
	f.admin = users.Request{Actor: perm.Actor{ID: adminID, Role: perm.RCONAdmin}, Pseudonym: "admin", RequestID: "req-admin"}
	f.sa = users.Request{Actor: perm.Actor{ID: saID, Role: perm.ServerAdmin}, Pseudonym: "sa", RequestID: "req-sa"}
	return f
}

// agent is a test agent with its own key.
type agent struct {
	id   *keys.Identity
	conn *websocket.Conn
}

func newAgent(t *testing.T) *agent {
	t.Helper()
	log := logging.New(&lockedBuf{}, "agent", config.Log{Format: "json"}).Root()
	id, err := keys.LoadOrCreateIdentity(log, filepath.Join(t.TempDir(), keys.AgentKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	return &agent{id: id}
}

// hello connects with the pinned panel fingerprint and sends hello; it
// returns the answer.
func (f *fixture) hello(a *agent, pinned string, h agentproto.Hello) agentproto.Envelope {
	f.t.Helper()
	cfg, err := agentproto.ClientConfig(a.id, pinned)
	if err != nil {
		f.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, "wss://"+f.addr+agentproto.Path, &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = c.CloseNow() })
	a.conn = c
	h.Name, h.Backend, h.Version = "host-1", "docker", buildinfo.Version
	data, _ := agentproto.Encode(agentproto.TypeHello, "", h)
	if err := c.Write(ctx, websocket.MessageText, data); err != nil {
		f.t.Fatal(err)
	}
	_, answer, err := c.Read(ctx)
	if err != nil {
		f.t.Fatalf("no answer to %+v: %v\n%s", h, err, f.logs.String())
	}
	var e agentproto.Envelope
	_ = json.Unmarshal(answer, &e)
	return e
}

func reason(e agentproto.Envelope) string {
	r, _ := agentproto.Payload[agentproto.Reject](e)
	return r.Reason
}

func (f *fixture) auditActions() []string {
	f.t.Helper()
	rows, err := f.st.DB.QueryContext(context.Background(), `SELECT action || ':' || result || ':' || payload FROM audit_log ORDER BY ts, rowid`)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func (f *fixture) setPairing(on bool) {
	f.t.Helper()
	v := "false"
	if on {
		v = "true"
	}
	if _, err := f.st.DB.ExecContext(context.Background(),
		`UPDATE settings SET value = ?, updated_at = ? WHERE key = ?`, v, f.clock.UTC().Format(store.TimeLayout), users.SettingFingerprintPairing); err != nil {
		f.t.Fatal(err)
	}
}

// TestPairingByToken covers way A of AGT-005 and AGT-012: only the RCON
// admin creates tokens; a token pairs one agent once; the agent comes back
// with its ID and key; another key with that ID is a pin mismatch.
func TestPairingByToken(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.CreateToken(ctx, f.sa); !errors.Is(err, users.ErrForbidden) {
		t.Fatalf("server admin creates a token: %v", err)
	}
	tok, err := f.svc.CreateToken(ctx, f.admin)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := agentproto.ParsePairingToken(tok.Token)
	if err != nil || pt.Addr != "panel.example:8443" || pt.Fingerprint != f.hub.Fingerprint() {
		t.Fatalf("token = %+v %v", pt, err)
	}
	if !strings.Contains(tok.Compose, "RCD_PAIRING_TOKEN: \""+tok.Token+"\"") || !strings.Contains(tok.Compose, "group_add") {
		t.Errorf("compose = %s", tok.Compose)
	}

	a := newAgent(t)
	e := f.hello(a, pt.Fingerprint, agentproto.Hello{Token: pt.Secret})
	w, _ := agentproto.Payload[agentproto.Welcome](e)
	if e.Type != agentproto.TypeWelcome || w.AgentID == "" {
		t.Fatalf("pairing = %+v", e)
	}
	got, err := f.svc.Get(ctx, f.admin, w.AgentID)
	if err != nil || got.Status != StatusActive || got.Fingerprint != a.id.Fingerprint() || got.PairedBy != f.admin.Actor.ID || !got.Connected {
		t.Errorf("agent = %+v %v", got, err)
	}

	// The token is used up, also for another agent.
	if e := f.hello(newAgent(t), pt.Fingerprint, agentproto.Hello{Token: pt.Secret}); reason(e) != agentproto.RejectUnknown {
		t.Errorf("second use: %+v", e)
	}
	// The agent comes back with its ID.
	if e := f.hello(a, pt.Fingerprint, agentproto.Hello{AgentID: w.AgentID}); e.Type != agentproto.TypeWelcome {
		t.Errorf("reconnect: %+v", e)
	}
	// Someone else with the ID but another key.
	if e := f.hello(newAgent(t), pt.Fingerprint, agentproto.Hello{AgentID: w.AgentID}); e.Type != agentproto.TypeReject {
		t.Errorf("stolen ID: %+v", e)
	}
	if !strings.Contains(f.logs.String(), `"reason":"pin_mismatch"`) || !strings.Contains(f.logs.String(), `"event":"agent.paired"`) {
		t.Errorf("events missing:\n%s", f.logs.String())
	}
	for _, line := range f.auditActions() {
		if strings.Contains(line, pt.Secret) {
			t.Errorf("token secret in the audit: %s", line)
		}
	}
	if strings.Contains(f.logs.String(), pt.Secret) {
		t.Error("token secret in the log")
	}
}

func TestExpiredToken(t *testing.T) {
	f := newFixture(t)
	tok, err := f.svc.CreateToken(context.Background(), f.admin)
	if err != nil {
		t.Fatal(err)
	}
	pt, _ := agentproto.ParsePairingToken(tok.Token)
	if _, err := f.st.DB.ExecContext(context.Background(), `UPDATE agent_tokens SET expires_at = ?`,
		time.Now().Add(-time.Minute).UTC().Format(store.TimeLayout)); err != nil {
		t.Fatal(err)
	}
	if e := f.hello(newAgent(t), pt.Fingerprint, agentproto.Hello{Token: pt.Secret}); reason(e) != agentproto.RejectUnknown {
		t.Errorf("expired token: %+v", e)
	}
	n, _ := f.st.Queries.CountPendingAgents(context.Background())
	var all int
	_ = f.st.DB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM agents`).Scan(&all)
	if all != 0 || n != 0 {
		t.Errorf("an expired token left %d agents", all)
	}
}

// TestRevoke covers Design 1.7 and AGT-012: a revoked agent gets the
// message revoked, is disconnected and is turned away afterwards.
func TestRevoke(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tok, _ := f.svc.CreateToken(ctx, f.admin)
	pt, _ := agentproto.ParsePairingToken(tok.Token)
	a := newAgent(t)
	e := f.hello(a, pt.Fingerprint, agentproto.Hello{Token: pt.Secret})
	w, _ := agentproto.Payload[agentproto.Welcome](e)

	if err := f.svc.Delete(ctx, f.sa, w.AgentID); !errors.Is(err, users.ErrForbidden) {
		t.Fatalf("server admin revokes: %v", err)
	}
	if err := f.svc.Delete(ctx, f.admin, w.AgentID); err != nil {
		t.Fatal(err)
	}
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, data, err := a.conn.Read(rctx)
	var msg agentproto.Envelope
	if err == nil {
		_ = json.Unmarshal(data, &msg)
	}
	if msg.Type != agentproto.TypeRevoked {
		t.Errorf("agent got %+v %v, want revoked", msg, err)
	}
	if _, _, err := a.conn.Read(rctx); err == nil {
		t.Error("session still open after revocation")
	}
	if e := f.hello(a, pt.Fingerprint, agentproto.Hello{AgentID: w.AgentID}); reason(e) != agentproto.RejectRevoked {
		t.Errorf("revoked agent comes back: %+v", e)
	}
	got, _ := f.svc.Get(ctx, f.admin, w.AgentID)
	if got.Status != StatusRevoked || got.RevokedAt == nil || got.Connected {
		t.Errorf("agent = %+v", got)
	}
	for _, want := range []string{`"event":"agent.revoked"`, `"reason":"revoked"`} {
		if !strings.Contains(f.logs.String(), want) {
			t.Errorf("%s missing", want)
		}
	}
	if !strings.Contains(strings.Join(f.auditActions(), "\n"), "agent.revoke:ok") {
		t.Error("revocation not audited")
	}
}

// TestPairingByFingerprint covers way B of AGT-005 and SEC-007: off by
// default; switched on, an agent waits for approval; at most 10 wait; the
// window closes itself after 30 minutes with an audit entry.
func TestPairingByFingerprint(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := newAgent(t)
	if e := f.hello(a, "", agentproto.Hello{}); reason(e) != agentproto.RejectUnknown {
		t.Fatalf("way B while off: %+v", e)
	}
	f.setPairing(true)
	if e := f.hello(a, "", agentproto.Hello{}); reason(e) != agentproto.RejectPending {
		t.Fatalf("way B while on: %+v", e)
	}
	list, _ := f.svc.List(ctx, f.admin)
	if len(list) != 1 || list[0].Status != StatusPending || list[0].Fingerprint != a.id.Fingerprint() {
		t.Fatalf("list = %+v", list)
	}
	if e := f.hello(a, "", agentproto.Hello{}); reason(e) != agentproto.RejectPending {
		t.Errorf("waiting agent: %+v", e)
	}

	// Pending pairings stay below the limit.
	for range PendingMax + 2 {
		f.hello(newAgent(t), "", agentproto.Hello{})
	}
	if n, _ := f.st.Queries.CountPendingAgents(ctx); n != PendingMax {
		t.Errorf("%d pending, want %d", n, PendingMax)
	}
	if !strings.Contains(f.logs.String(), `"reason":"pending_pairings"`) {
		t.Error("agent.limit pending_pairings missing")
	}

	if _, err := f.svc.Approve(ctx, f.sa, list[0].ID); !errors.Is(err, users.ErrForbidden) {
		t.Fatalf("server admin approves: %v", err)
	}
	if _, err := f.svc.Approve(ctx, f.admin, list[0].ID); err != nil {
		t.Fatal(err)
	}
	e := f.hello(a, "", agentproto.Hello{})
	w, _ := agentproto.Payload[agentproto.Welcome](e)
	if e.Type != agentproto.TypeWelcome || w.AgentID != list[0].ID {
		t.Errorf("approved agent: %+v", e)
	}

	// After 30 minutes the window closes itself.
	f.clock = f.clock.Add(users.PairingWindow + time.Second)
	if f.svc.PairingOpen(ctx) {
		t.Error("window still open after 30 minutes")
	}
	if err := f.svc.closeExpiredPairing(ctx); err != nil {
		t.Fatal(err)
	}
	var on bool
	_ = f.st.GlobalSetting(ctx, users.SettingFingerprintPairing, &on)
	if on || !strings.Contains(strings.Join(f.auditActions(), "\n"), `window_expired`) {
		t.Errorf("setting %v, audit %v", on, f.auditActions())
	}

	// A pending pairing older than 24 h is dropped.
	f.clock = f.clock.Add(PendingTimeout)
	if list, _ := f.svc.List(ctx, f.admin); len(list) != 1 {
		t.Errorf("stale pending pairings kept: %d agents", len(list))
	}
}

func TestTurnDownPending(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.setPairing(true)
	f.hello(newAgent(t), "", agentproto.Hello{})
	list, _ := f.svc.List(ctx, f.admin)
	if err := f.svc.Delete(ctx, f.admin, list[0].ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := f.svc.List(ctx, f.admin); len(list) != 0 {
		t.Errorf("turned down pairing kept: %+v", list)
	}
	if !strings.Contains(strings.Join(f.auditActions(), "\n"), "agent.reject:ok") {
		t.Error("turning down not audited")
	}
	if err := f.svc.Delete(ctx, f.admin, list[0].ID); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
}
