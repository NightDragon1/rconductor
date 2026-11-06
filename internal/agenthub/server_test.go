// SPDX-License-Identifier: AGPL-3.0-or-later

package agenthub

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/buildinfo"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/logtest"
	"github.com/NightDragon1/rconductor/internal/ops/keys"
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

// admitKnown admits agents whose key it knows.
type admitKnown struct {
	mu   sync.Mutex
	keys map[string]string // fingerprint → agent ID
}

func (a *admitKnown) Admit(_ context.Context, h agentproto.Hello, key ed25519.PublicKey, _ string) (Admission, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id, ok := a.keys[keys.Fingerprint(key)]
	switch {
	case !ok && h.AgentID != "":
		return Admission{PinMismatch: true}, nil
	case !ok:
		return Admission{Reject: agentproto.RejectUnknown}, nil
	}
	return Admission{AgentID: id}, nil
}

type fixture struct {
	hub   *Hub
	addr  string
	logs  *lockedBuf
	panel *keys.Identity
	known *admitKnown
}

func newFixture(t *testing.T, throttle ThrottleOptions) *fixture {
	t.Helper()
	return newFixtureWith(t, Options{Throttle: throttle})
}

func newFixtureWith(t *testing.T, opts Options) *fixture {
	t.Helper()
	logs := &lockedBuf{}
	log := logging.New(logs, "panel", config.Log{Format: "json"}).Module("agent")
	panel, err := keys.LoadOrCreateIdentity(log, filepath.Join(t.TempDir(), keys.PanelKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	known := &admitKnown{keys: map[string]string{}}
	opts.Registry = known
	h := New(panel, log, opts)
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() { done <- h.Serve(ctx, "127.0.0.1:0", func(a net.Addr) { ready <- a }) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	return &fixture{hub: h, addr: (<-ready).String(), logs: logs, panel: panel, known: known}
}

func agentKey(t *testing.T) *keys.Identity {
	t.Helper()
	log := logging.New(&lockedBuf{}, "agent", config.Log{Format: "json"}).Root()
	id, err := keys.LoadOrCreateIdentity(log, filepath.Join(t.TempDir(), keys.AgentKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *fixture) dial(t *testing.T, id *keys.Identity, pinned string) (*websocket.Conn, error) {
	t.Helper()
	cfg, err := agentproto.ClientConfig(id, pinned)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, "wss://"+f.addr+agentproto.Path, &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Cleanup(func() { _ = c.CloseNow() })
	}
	return c, err
}

func hello(t *testing.T, c *websocket.Conn, h agentproto.Hello) agentproto.Envelope {
	t.Helper()
	if h.Version == "" {
		h.Version = buildinfo.Version
	}
	h.Backend, h.Name = "docker", "host-1"
	data, _ := agentproto.Encode(agentproto.TypeHello, "", h)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
	_, answer, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("no answer to hello: %v", err)
	}
	var e agentproto.Envelope
	if err := json.Unmarshal(answer, &e); err != nil {
		t.Fatal(err)
	}
	return e
}

// TestPinnedKeys covers AGT-012 on the agent port: TLS 1.3 only; an agent
// that pinned another panel key refuses the handshake and both sides
// know why; a known agent is welcomed, an unknown one rejected.
func TestPinnedKeys(t *testing.T) {
	f := newFixture(t, ThrottleOptions{})
	agent := agentKey(t)
	f.known.keys[agent.Fingerprint()] = "agent-1"

	c, err := f.dial(t, agent, f.panel.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	e := hello(t, c, agentproto.Hello{AgentID: "agent-1"})
	w, _ := agentproto.Payload[agentproto.Welcome](e)
	if e.Type != agentproto.TypeWelcome || w.AgentID != "agent-1" || e.Ref == "" {
		t.Errorf("answer = %+v", e)
	}
	logtest.Wait(t, f.logs, `"event":"agent.connected"`)

	other := agentKey(t) // another panel's fingerprint
	if _, err := f.dial(t, agent, other.Fingerprint()); err == nil || !errors.Is(err, agentproto.ErrPinMismatch) && !strings.Contains(err.Error(), "pinned") {
		t.Errorf("agent accepted a panel with another key: %v", err)
	}
	logtest.Wait(t, f.logs, `"reason":"pin_mismatch"`)

	stranger := agentKey(t)
	c, err = f.dial(t, stranger, f.panel.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	if e := hello(t, c, agentproto.Hello{}); e.Type != agentproto.TypeReject {
		t.Errorf("unknown agent: %+v", e)
	}
	// An agent that claims a known ID with another key is a pin mismatch.
	c, err = f.dial(t, stranger, f.panel.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	if e := hello(t, c, agentproto.Hello{AgentID: "agent-1"}); e.Type != agentproto.TypeReject {
		t.Errorf("stolen agent ID: %+v", e)
	}
	logtest.WaitCount(t, f.logs, `"reason":"pin_mismatch"`, 2)

	cfg, _ := agentproto.ClientConfig(agent, f.panel.Fingerprint())
	cfg.MaxVersion = tls.VersionTLS12
	cfg.MinVersion = tls.VersionTLS12
	if conn, err := tls.Dial("tcp", f.addr, cfg); err == nil {
		_ = conn.Close()
		t.Error("TLS 1.2 accepted")
	}
}

// TestVersionRule covers AGT-013: another major version is rejected with
// reason version, with a hint for the agent log.
func TestVersionRule(t *testing.T) {
	f := newFixture(t, ThrottleOptions{})
	agent := agentKey(t)
	f.known.keys[agent.Fingerprint()] = "agent-1"
	c, err := f.dial(t, agent, f.panel.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	e := hello(t, c, agentproto.Hello{AgentID: "agent-1", Version: "99.0.0"})
	r, _ := agentproto.Payload[agentproto.Reject](e)
	if e.Type != agentproto.TypeReject || r.Reason != agentproto.RejectVersion || !strings.Contains(r.Message, "99.0.0") {
		t.Errorf("answer = %+v %+v", e, r)
	}
	logtest.Wait(t, f.logs, `"event":"agent.rejected","reason":"version"`)
}

// TestOversizedMessage covers SEC-007: a message over 1 MiB ends the
// session (agent.limit and agent.disconnected).
func TestOversizedMessage(t *testing.T) {
	f := newFixture(t, ThrottleOptions{})
	agent := agentKey(t)
	f.known.keys[agent.Fingerprint()] = "agent-1"
	c, err := f.dial(t, agent, f.panel.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	if e := hello(t, c, agentproto.Hello{AgentID: "agent-1"}); e.Type != agentproto.TypeWelcome {
		t.Fatalf("answer = %+v", e)
	}
	big := bytes.Repeat([]byte("x"), agentproto.MaxAgentMessage+1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.Write(ctx, websocket.MessageText, big)
	if _, _, err := c.Read(ctx); err == nil {
		t.Error("session still open after an oversized message")
	}
	logtest.Wait(t, f.logs, `"event":"agent.limit","agent_id":"agent-1","reason":"message_size"`)
	logtest.Wait(t, f.logs, `"reason":"too_large"`)
	if f.hub.Connected("agent-1") {
		t.Error("agent still connected")
	}
}

// TestIdleConnectionEnds covers T-017: a client that finishes the handshake
// and one plain request cannot keep the connection without a session.
func TestIdleConnectionEnds(t *testing.T) {
	f := newFixtureWith(t, Options{HelloTimeout: 300 * time.Millisecond})
	cfg, err := agentproto.ClientConfig(agentKey(t), f.panel.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	conn, err := tls.Dial("tcp", f.addr, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte("GET /x HTTP/1.1\r\nHost: x\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, conn); err != nil {
		t.Errorf("the idle connection stayed open: %v", err)
	}
}

// TestConnectionThrottle covers SEC-007: quick connection attempts of one
// address are cut off after the burst.
func TestConnectionThrottle(t *testing.T) {
	f := newFixture(t, ThrottleOptions{PerMinute: 1, Burst: 3})
	refused := 0
	for range 6 {
		var d net.Dialer
		c, err := d.DialContext(context.Background(), "tcp", f.addr)
		if err != nil {
			refused++
			continue
		}
		_ = c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		// A throttled connection is closed at once: the read ends with EOF,
		// a handshake would wait for our ClientHello and time out.
		var b [1]byte
		_, err = c.Read(b[:])
		var ne net.Error
		if err != nil && (!errors.As(err, &ne) || !ne.Timeout()) {
			refused++
		}
		_ = c.Close()
	}
	if refused != 3 {
		t.Errorf("refused %d of 6 attempts, want 3", refused)
	}
	logtest.Wait(t, f.logs, `"event":"agent.limit","reason":"connection_rate"`)
	if n := logtest.WaitCount(t, f.logs, `"connection_rate"`, 1); n != 1 {
		t.Errorf("connection_rate logged %d times, want once", n)
	}
}

// TestProtocolBreak: a second hello or a type the agent may not send ends
// the session with reason protocol.
func TestProtocolBreak(t *testing.T) {
	f := newFixture(t, ThrottleOptions{})
	agent := agentKey(t)
	f.known.keys[agent.Fingerprint()] = "agent-1"
	c, err := f.dial(t, agent, f.panel.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	hello(t, c, agentproto.Hello{AgentID: "agent-1"})
	data, _ := agentproto.Encode(agentproto.TypeContainerAction, "", agentproto.ContainerAction{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.Write(ctx, websocket.MessageText, data)
	if _, _, err := c.Read(ctx); err == nil {
		t.Error("session still open")
	}
	logtest.Wait(t, f.logs, `"reason":"protocol"`)
}

// TestMessagesAndDisconnect: messages of an admitted agent reach the
// receiver; the end of its session is reported once (NFR-005).
func TestMessagesAndDisconnect(t *testing.T) {
	f := newFixture(t, ThrottleOptions{})
	agent := agentKey(t)
	f.known.keys[agent.Fingerprint()] = "agent-1"
	var mu sync.Mutex
	var got []string
	gone := make(chan string, 2)
	f.hub.OnMessage(func(id string, e agentproto.Envelope) {
		mu.Lock()
		got = append(got, id+":"+e.Type)
		mu.Unlock()
	})
	f.hub.OnDisconnect(func(id string) { gone <- id })
	c, err := f.dial(t, agent, f.panel.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	hello(t, c, agentproto.Hello{AgentID: "agent-1"})
	data, _ := agentproto.Encode(agentproto.TypeSnapshot, "", agentproto.Snapshot{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
	logtest.Wait(t, f.logs, `"event":"agent.connected"`)
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	if len(got) != 1 || got[0] != "agent-1:snapshot" {
		t.Errorf("messages = %v", got)
	}
	mu.Unlock()
	_ = c.Close(websocket.StatusNormalClosure, "bye")
	select {
	case id := <-gone:
		if id != "agent-1" {
			t.Errorf("disconnect of %s", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no disconnect reported")
	}
}

// TestBeforeWelcome covers revision 2.3 no. 13: until welcome a message
// may have at most 16 KiB, and the hello must come within the deadline
// after the TLS handshake.
func TestBeforeWelcome(t *testing.T) {
	f := newFixtureWith(t, Options{HelloTimeout: 300 * time.Millisecond})
	agent := agentKey(t)
	f.known.keys[agent.Fingerprint()] = "agent-1"

	c, err := f.dial(t, agent, f.panel.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	big, _ := agentproto.Encode(agentproto.TypeHello, "", agentproto.Hello{AgentID: "agent-1", Name: strings.Repeat("x", agentproto.MaxHelloMessage)})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.Write(ctx, websocket.MessageText, big)
	if _, _, err := c.Read(ctx); err == nil {
		t.Error("oversized hello accepted")
	}
	logtest.Wait(t, f.logs, `"event":"agent.limit","reason":"message_size"`)

	c, err = f.dial(t, agent, f.panel.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, _, err := c.Read(ctx); err == nil {
		t.Error("connection without hello kept open")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("closed after %v, want about 300 ms", d)
	}
	logtest.Wait(t, f.logs, `"event":"agent.disconnected","reason":"timeout"`)

	// A normal hello still fits, and after welcome the 1 MiB limit holds.
	c, err = f.dial(t, agent, f.panel.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	if e := hello(t, c, agentproto.Hello{AgentID: "agent-1"}); e.Type != agentproto.TypeWelcome {
		t.Fatalf("hello = %+v", e)
	}
	snap, _ := agentproto.Encode(agentproto.TypeSnapshot, "", agentproto.Snapshot{Containers: []agentproto.Container{{Name: strings.Repeat("y", 100<<10)}}})
	if err := c.Write(ctx, websocket.MessageText, snap); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if !f.hub.Connected("agent-1") {
		t.Error("a 100 KiB message after welcome ended the session")
	}
}

// TestOtherRequestsEndTheConnection covers T-017: anything but the upgrade to
// an agent session ends with its answer, a client cannot stay with new requests.
func TestOtherRequestsEndTheConnection(t *testing.T) {
	f := newFixture(t, ThrottleOptions{})
	cfg, err := agentproto.ClientConfig(agentKey(t), f.panel.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	c, err := tls.Dial("tcp", f.addr, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if _, err := io.WriteString(c, "GET /other HTTP/1.1\r\nHost: test\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	answer, err := io.ReadAll(c)
	if err != nil {
		t.Fatalf("connection stayed open: %v", err)
	}
	if !bytes.Contains(answer, []byte("404")) {
		t.Errorf("answer = %q", answer)
	}
}
