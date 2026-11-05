// SPDX-License-Identifier: AGPL-3.0-or-later

package agent

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/agenthub"
	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/agents"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logtest"
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

// fakeBackend counts sessions and answers every order with ok.
type fakeBackend struct {
	mu     sync.Mutex
	runs   int
	orders []string
}

func (b *fakeBackend) Name() string           { return "docker" }
func (b *fakeBackend) Capabilities() []string { return []string{agentproto.CapMetrics} }

func (b *fakeBackend) Run(ctx context.Context, out Sender) error {
	b.mu.Lock()
	b.runs++
	b.mu.Unlock()
	if err := out.Send(ctx, agentproto.TypeSnapshot, "", agentproto.Snapshot{Containers: []agentproto.Container{}}); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

func (b *fakeBackend) Handle(ctx context.Context, e agentproto.Envelope, out Sender) {
	b.mu.Lock()
	b.orders = append(b.orders, e.Type)
	b.mu.Unlock()
	_ = out.Send(ctx, agentproto.TypeResult, e.ID, agentproto.Result{Code: agentproto.CodeOK})
}

type panel struct {
	t     *testing.T
	st    *store.Store
	svc   *agents.Service
	hub   *agenthub.Hub
	addr  string
	logs  *lockedBuf
	admin users.Request
	stop  func()
}

func newPanel(t *testing.T) *panel {
	t.Helper()
	st := storetest.New(t)
	if err := users.EnsureAuditDefaults(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	logs := &lockedBuf{}
	log := logging.New(logs, "panel", config.Log{Format: "json"}).Module("agent")
	key, err := keys.LoadOrCreateIdentity(log, filepath.Join(t.TempDir(), keys.PanelKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	p := &panel{t: t, st: st, logs: logs}
	p.svc = agents.NewService(st, log, "")
	p.hub = agenthub.New(key, log, agenthub.Options{Registry: p.svc, Throttle: agenthub.ThrottleOptions{PerMinute: 1000, Burst: 1000}})
	p.svc.SetHub(p.hub)
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() { done <- p.hub.Serve(ctx, "127.0.0.1:0", func(a net.Addr) { ready <- a }) }()
	p.stop = func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
	}
	t.Cleanup(p.stop)
	p.addr = (<-ready).String()
	adminID := storetest.CreateUser(t, st, storetest.User{Pseudonym: "admin", Role: "rcon_admin"})
	p.admin = users.Request{Actor: perm.Actor{ID: adminID, Role: perm.RCONAdmin}, Pseudonym: "admin", RequestID: "r"}
	return p
}

func (p *panel) token() string {
	p.t.Helper()
	tok, err := p.svc.CreateToken(context.Background(), p.admin)
	if err != nil {
		p.t.Fatal(err)
	}
	return tok.Token
}

type runner struct {
	a       *Agent
	logs    *lockedBuf
	backend *fakeBackend
	cancel  context.CancelFunc
	done    chan error
	once    sync.Once
	err     error
}

func start(t *testing.T, cfg config.Agent) *runner {
	t.Helper()
	logs := &lockedBuf{}
	log := logging.New(logs, "agent", config.Log{Format: "json"}).Root()
	b := &fakeBackend{}
	a, err := New(cfg, log, b)
	if err != nil {
		t.Fatal(err)
	}
	a.backoff = func(int) time.Duration { return 20 * time.Millisecond }
	ctx, cancel := context.WithCancel(context.Background())
	r := &runner{a: a, logs: logs, backend: b, cancel: cancel, done: make(chan error, 1)}
	go func() { r.done <- a.Run(ctx) }()
	t.Cleanup(r.stop)
	return r
}

func (r *runner) stop() {
	r.once.Do(func() {
		r.cancel()
		if err := <-r.done; err != nil {
			r.err = err
		}
	})
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func agentCfg(t *testing.T, dir string) config.Agent {
	t.Helper()
	return config.Agent{Name: "host-1", SecretsDir: dir, LabelKey: "rcon.manage", LabelValue: "true", Log: config.Log{Format: "json"}}
}

// TestPairAndReconnect covers AGT-011 and AGT-012 with way A: the agent
// pairs with the token, keeps its ID and the pinned panel key in /secrets
// and reconnects with them; the token is ignored afterwards.
func TestPairAndReconnect(t *testing.T) {
	p := newPanel(t)
	dir := t.TempDir()
	cfg := agentCfg(t, dir)
	cfg.PairingToken, cfg.PanelAddr = p.token(), p.addr
	r := start(t, cfg)
	waitFor(t, "pairing", func() bool { return r.a.State().AgentID != "" })
	id := r.a.State().AgentID
	waitFor(t, "session", func() bool { return p.hub.Connected(id) })
	logtest.Wait(t, r.logs, `"event":"agent.handshake"`)
	st, _ := loadState(dir)
	if st.AgentID != id || st.PanelFingerprint != p.hub.Fingerprint() || st.TokenHash == "" {
		t.Errorf("state = %+v", st)
	}
	if fi, err := os.Stat(filepath.Join(dir, StateFile)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("state file mode: %v %v", fi, err)
	}
	if strings.Contains(r.logs.String(), cfg.PairingToken) {
		t.Error("token in the agent log")
	}
	r.stop()

	// Same token still in the environment: the agent reconnects by ID.
	r = start(t, cfg)
	waitFor(t, "reconnect", func() bool { return p.hub.Connected(id) })
	if r.a.State().AgentID != id {
		t.Errorf("agent ID changed to %s", r.a.State().AgentID)
	}
	list, _ := p.svc.List(context.Background(), p.admin)
	if len(list) != 1 {
		t.Errorf("%d agents, want 1", len(list))
	}
}

// TestRevokedAgentStays covers Design 1.7: after revoked the agent does
// not reconnect, also after a restart.
func TestRevokedAgentStays(t *testing.T) {
	p := newPanel(t)
	dir := t.TempDir()
	cfg := agentCfg(t, dir)
	cfg.PairingToken, cfg.PanelAddr = p.token(), p.addr
	r := start(t, cfg)
	waitFor(t, "session", func() bool { return r.a.State().AgentID != "" && p.hub.Connected(r.a.State().AgentID) })
	if err := p.svc.Delete(context.Background(), p.admin, r.a.State().AgentID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "revocation", func() bool { st, _ := loadState(dir); return st.Revoked })
	logtest.Wait(t, r.logs, "not reconnecting")
	r.stop()
	before := strings.Count(p.logs.String(), `"msg":"agent rejected"`)
	r = start(t, cfg)
	time.Sleep(200 * time.Millisecond)
	if n := logtest.WaitCount(t, p.logs, `"msg":"agent rejected"`, before); n != before {
		t.Errorf("revoked agent tried again (%d rejections)", n-before)
	}
	// A new token pairs it again.
	cfg.PairingToken = p.token()
	r.stop()
	r = start(t, cfg)
	waitFor(t, "new pairing", func() bool {
		st, _ := loadState(dir)
		return !st.Revoked && st.AgentID != "" && p.hub.Connected(st.AgentID)
	})
}

// TestPinnedPanel covers AGT-012: a panel with another key is refused by
// the agent, with pin_mismatch in the agent log.
func TestPinnedPanel(t *testing.T) {
	p := newPanel(t)
	other := newPanel(t)
	dir := t.TempDir()
	cfg := agentCfg(t, dir)
	cfg.PairingToken, cfg.PanelAddr = p.token(), p.addr
	r := start(t, cfg)
	waitFor(t, "pairing", func() bool { return r.a.State().AgentID != "" })
	r.stop()
	cfg.PanelAddr = other.addr // the address now leads to another panel
	r = start(t, cfg)
	logtest.Wait(t, r.logs, `"event":"agent.rejected","reason":"pin_mismatch"`)
	logtest.Wait(t, other.logs, `"reason":"pin_mismatch"`)
}

// TestWayB covers pairing by fingerprint from the agent side: the agent
// logs both fingerprints, pins the panel key at the first contact, waits
// and is admitted after approval.
func TestWayB(t *testing.T) {
	p := newPanel(t)
	ctx := context.Background()
	if _, err := p.st.DB.ExecContext(ctx, `UPDATE settings SET value = 'true', updated_at = ? WHERE key = ?`,
		store.Now(), users.SettingFingerprintPairing); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cfg := agentCfg(t, dir)
	cfg.PanelAddr = p.addr
	r := start(t, cfg)
	logtest.Wait(t, r.logs, "pairing waits for approval")
	logtest.Wait(t, r.logs, `"panel_fingerprint":"`+p.hub.Fingerprint()+`"`, `"fingerprint":"`+r.a.Fingerprint()+`"`)
	if st, _ := loadState(dir); st.PanelFingerprint != p.hub.Fingerprint() {
		t.Errorf("panel key not pinned at the first contact: %+v", st)
	}
	list, _ := p.svc.List(ctx, p.admin)
	if len(list) != 1 {
		t.Fatalf("pending = %+v", list)
	}
	if _, err := p.svc.Approve(ctx, p.admin, list[0].ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "admission", func() bool { return p.hub.Connected(list[0].ID) })
}

// TestUnsupportedOrder covers AGT-004 on the agent: an order outside
// Design 1.5 is answered with unsupported and never reaches the backend.
func TestUnsupportedOrder(t *testing.T) {
	p := newPanel(t)
	cfg := agentCfg(t, t.TempDir())
	cfg.PairingToken, cfg.PanelAddr = p.token(), p.addr
	r := start(t, cfg)
	waitFor(t, "session", func() bool { return r.a.State().AgentID != "" && p.hub.Connected(r.a.State().AgentID) })
	codes := map[string]int{}
	for _, typ := range []string{"container.exec", "container.create", agentproto.TypeLogsTail} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		res, err := p.hub.Request(ctx, r.a.State().AgentID, typ, map[string]string{"container": "x"})
		cancel()
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		codes[res.Code]++
	}
	if codes[agentproto.CodeUnsupported] != 2 || codes[agentproto.CodeOK] != 1 {
		t.Errorf("results = %v", codes)
	}
	r.backend.mu.Lock()
	defer r.backend.mu.Unlock()
	if len(r.backend.orders) != 1 || r.backend.orders[0] != agentproto.TypeLogsTail {
		t.Errorf("backend got %v", r.backend.orders)
	}
	logtest.Wait(t, r.logs, `"event":"agent.unsupported"`)
}

// TestNoPanel covers Design 5.5: without a token the agent waits with a
// hint in the log; the operator adds the token and restarts it.
func TestNoPanel(t *testing.T) {
	logs := &lockedBuf{}
	r, err := New(agentCfg(t, t.TempDir()), logging.New(logs, "agent", config.Log{Format: "json"}).Root(), &fakeBackend{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	logtest.Wait(t, logs, "waiting for a pairing token", "RCD_PAIRING_TOKEN")
	select {
	case err := <-done:
		t.Fatalf("the agent stopped instead of waiting: %v", err)
	default:
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("run = %v", err)
	}
}

// TestAgentWithoutPanel: without token and panel address the agent creates
// its key pair, says what is missing and waits (Design 5.5).
func TestAgentWithoutPanel(t *testing.T) {
	var out lockedBuf
	cfg := config.Agent{SecretsDir: t.TempDir(), DockerHost: "unix:///nonexistent.sock", LabelKey: "rcon.manage", LabelValue: "true",
		Log: config.Log{Format: "json"}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Main(ctx, cfg, &out) }()
	logtest.Wait(t, &out, `"comp":"agent"`, `"event":"key.created"`, `"event":"app.ready"`, "RCD_PAIRING_TOKEN")
	cancel()
	if err := <-done; err != nil {
		t.Errorf("run = %v", err)
	}
}

// TestVersionHint covers the agent side of AGT-013: a reject with reason
// version puts the panel's hint and the agent's version into the agent log.
func TestVersionHint(t *testing.T) {
	logs := &lockedBuf{}
	a, err := New(agentCfg(t, t.TempDir()), logging.New(logs, "agent", config.Log{Format: "json"}).Root(), &fakeBackend{})
	if err != nil {
		t.Fatal(err)
	}
	reason := a.rejected(agentproto.Reject{Reason: agentproto.RejectVersion, Message: "agent 2.0.0 and panel 1.4.0 differ in the major version"}, nil)
	if reason != agentproto.RejectVersion {
		t.Errorf("reason = %s", reason)
	}
	for _, want := range []string{`"event":"agent.rejected"`, `"reason":"version"`, "differ in the major version", `"version":"`} {
		logtest.Wait(t, logs, want)
	}
}
