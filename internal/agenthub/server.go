// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agenthub is the agent hub of the panel (Architecture 4): it runs
// the agent port with TLS 1.3 and pinned keys, admits agents and keeps
// one WebSocket session per agent (Design 1). Everything an agent sends
// is foreign data (SEC-006); the port is bounded against floods (SEC-007).
package agenthub

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/buildinfo"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/netlimit"
	"github.com/NightDragon1/rconductor/internal/ops/keys"
)

// Timing of a session (Design 1.1).
const (
	PingInterval     = 30 * time.Second
	pongTimeout      = 30 * time.Second // no pong 60 s after the last one: gone
	handshakeTimeout = 10 * time.Second
	// helloTimeout counts from the end of the TLS handshake (revision 2.3
	// no. 13).
	helloTimeout = 10 * time.Second
	// Connections of the agent port in total and per client address, far
	// above the number of hosts of one panel (T-017).
	acceptPause            = 100 * time.Millisecond
	maxAgentConns          = 1024
	maxAgentConnsPerClient = 64
)

// handshakeDone holds when the TLS handshake of a connection ended.
type handshakeDone struct{}

// Disconnect reasons of agent.disconnected (revision 2.3 candidate 5).
const (
	ReasonClosed   = "closed"
	ReasonTimeout  = "timeout"
	ReasonRevoked  = "revoked"
	ReasonTooLarge = "too_large"
	ReasonProtocol = "protocol"
)

// Errors of Request.
var (
	// ErrNotConnected means the agent has no session.
	ErrNotConnected = errors.New("agent not connected")
	// ErrBusy means too many orders wait for the agent.
	ErrBusy = errors.New("too many orders wait for the agent")
)

// maxPending bounds the orders waiting for results per agent.
const maxPending = 64

// Admission is the answer of the registry to a hello.
type Admission struct {
	AgentID string
	// Reject is set when the agent is refused (agentproto.Reject*).
	Reject string
	// Pinned is false when the key does not match the key stored for the
	// claimed agent ID (pin_mismatch).
	PinMismatch bool
	// Paired is set when this hello completed a pairing.
	Paired bool
}

// Registry decides which agents are admitted; the agent administration
// implements it (pairing, revocation).
type Registry interface {
	Admit(ctx context.Context, h agentproto.Hello, key ed25519.PublicKey, remote string) (Admission, error)
}

// rejectAll admits nobody; it stands in until a registry is set.
type rejectAll struct{}

func (rejectAll) Admit(context.Context, agentproto.Hello, ed25519.PublicKey, string) (Admission, error) {
	return Admission{Reject: agentproto.RejectUnknown}, nil
}

// Options tune the hub; zero values take the defaults.
type Options struct {
	Registry Registry
	// Throttle bounds connection attempts per source address.
	Throttle ThrottleOptions
	// MaxConnections and MaxClientConnections bound the open connections
	// in all and per client; zero takes the default.
	MaxConnections, MaxClientConnections int
	// Ping overrides PingInterval, for tests.
	Ping time.Duration
	// HelloTimeout overrides the 10 s for the hello, for tests.
	HelloTimeout time.Duration
}

func orDefault(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

// Hub runs the agent port.
type Hub struct {
	id       *keys.Identity
	log      *slog.Logger
	registry Registry
	throttle *throttle
	ping     time.Duration
	hello    time.Duration
	// maxConns and maxClientConns bound the agent port.
	maxConns, maxClientConns int

	mu       sync.Mutex
	sessions map[string]*Session
	// onMessage gets every message of an admitted agent except results.
	onMessage func(agentID string, e agentproto.Envelope)
	// onDisconnect is told when an agent's last session ends.
	onDisconnect func(agentID string)
}

// OnDisconnect sets the receiver of ended sessions; call it before Serve.
func (h *Hub) OnDisconnect(f func(agentID string)) { h.onDisconnect = f }

// OnMessage sets the receiver of snapshots, events, metrics and log lines;
// call it before Serve.
func (h *Hub) OnMessage(f func(agentID string, e agentproto.Envelope)) { h.onMessage = f }

// Request sends an order to an agent and waits for its result until ctx
// ends (Design 1.5).
func (h *Hub) Request(ctx context.Context, agentID, typ string, payload any) (agentproto.Result, error) {
	h.mu.Lock()
	s := h.sessions[agentID]
	h.mu.Unlock()
	if s == nil {
		return agentproto.Result{}, ErrNotConnected
	}
	return s.request(ctx, typ, payload, nil)
}

// RequestData sends an order whose answer brings data before its result,
// such as logs.tail: those messages go to collect, not to the general
// receiver (Design 1.5).
func (h *Hub) RequestData(ctx context.Context, agentID, typ string, payload any, collect func(agentproto.Envelope)) (agentproto.Result, error) {
	h.mu.Lock()
	s := h.sessions[agentID]
	h.mu.Unlock()
	if s == nil {
		return agentproto.Result{}, ErrNotConnected
	}
	return s.request(ctx, typ, payload, collect)
}

// New returns a hub with the panel identity.
func New(id *keys.Identity, log *slog.Logger, opts Options) *Hub {
	if opts.Registry == nil {
		opts.Registry = rejectAll{}
	}
	if opts.Ping <= 0 {
		opts.Ping = PingInterval
	}
	if opts.HelloTimeout <= 0 {
		opts.HelloTimeout = helloTimeout
	}
	return &Hub{
		id: id, log: log, registry: opts.Registry, throttle: newThrottle(opts.Throttle), ping: opts.Ping, hello: opts.HelloTimeout,
		maxConns: orDefault(opts.MaxConnections, maxAgentConns), maxClientConns: orDefault(opts.MaxClientConnections, maxAgentConnsPerClient),
		sessions: map[string]*Session{},
	}
}

// Fingerprint is the fingerprint of the panel key, for pairing tokens and
// the pairing dialog.
func (h *Hub) Fingerprint() string { return h.id.Fingerprint() }

// Serve listens on addr until ctx ends; ready gets the bound address.
func (h *Hub) Serve(ctx context.Context, addr string, ready func(net.Addr)) error {
	cfg, err := agentproto.ServerConfig(h.id)
	if err != nil {
		return err
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	tl := newHandshakeListener(ctx, netlimit.New(ln, netlimit.Options{Max: h.maxConns, PerClient: h.maxClientConns, Log: h.log}), cfg, h)
	mux := http.NewServeMux()
	mux.HandleFunc(agentproto.Path, h.handle)
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		// A connection that made no agent session goes away, also one that
		// announced a body and went silent (C1); upgraded sessions have
		// left the server by then.
		ReadTimeout: h.hello,
		IdleTimeout: h.hello,
		ErrorLog:    slog.NewLogLogger(h.log.Handler(), slog.LevelDebug),
		BaseContext: func(net.Listener) context.Context { return ctx },
		// The listener hands out connections right after their TLS
		// handshake; the hello deadline starts there.
		ConnContext: func(ctx context.Context, _ net.Conn) context.Context {
			return context.WithValue(ctx, handshakeDone{}, time.Now())
		},
	}
	// Only the upgrade to an agent session is of use here, so a connection
	// that gets anything else ends with its answer (T-017).
	srv.SetKeepAlivesEnabled(false)
	if ready != nil {
		ready(ln.Addr())
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
		h.closeAll()
	}()
	if err := srv.Serve(tl); err != nil && !errors.Is(err, http.ErrServerClosed) && ctx.Err() == nil {
		return err
	}
	return nil
}

// handshakeListener accepts TCP connections, throttles them per source
// address and finishes the TLS handshake before http.Server sees them, so
// that refused attempts are logged with their reason.
type handshakeListener struct {
	net.Listener
	conns chan net.Conn
	done  <-chan struct{}
	// stopped closes when the accept loop has ended; conns stays open,
	// because handshakes may still be sending on it (V-4).
	stopped chan struct{}
}

func newHandshakeListener(ctx context.Context, ln net.Listener, cfg *tls.Config, h *Hub) *handshakeListener {
	l := &handshakeListener{Listener: ln, conns: make(chan net.Conn), done: ctx.Done(), stopped: make(chan struct{})}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	go func() {
		defer close(l.stopped)
		for {
			c, err := ln.Accept()
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return
			}
			if err != nil {
				// Out of file descriptors and the like pass.
				h.log.Warn("agent port cannot accept", slog.Any("err", err))
				select {
				case <-time.After(acceptPause):
				case <-ctx.Done():
				}
				continue
			}
			remote := hostOf(c.RemoteAddr())
			// Rotating inside an IPv6 /64 is one source (T-017).
			if ip, err := netip.ParseAddr(remote); err == nil {
				remote = netlimit.ClientKey(ip).String()
			}
			if !h.throttle.allow(remote) {
				h.throttle.logOnce(h.log, remote)
				_ = c.Close()
				continue
			}
			go l.handshake(ctx, c, cfg, h, remote)
		}
	}()
	return l
}

func (l *handshakeListener) handshake(ctx context.Context, c net.Conn, cfg *tls.Config, h *Hub, remote string) {
	tc := tls.Server(c, cfg)
	hctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()
	if err := tc.HandshakeContext(hctx); err != nil {
		_ = c.Close()
		if strings.Contains(err.Error(), "bad certificate") {
			// The agent refused our key: it has another panel pinned.
			h.log.Warn("agent refused the panel key", logging.Event(logging.AgentRejected),
				slog.String(logging.KeyReason, "pin_mismatch"), slog.String("remote", remote))
		} else {
			h.log.Debug("agent TLS handshake failed", slog.String("remote", remote), slog.Any("err", err))
		}
		return
	}
	select {
	case l.conns <- tc:
	case <-l.done:
		_ = tc.Close()
	case <-l.stopped:
		_ = tc.Close()
	}
}

func (l *handshakeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.stopped:
		return nil, net.ErrClosed
	}
}

func hostOf(a net.Addr) string {
	host, _, err := net.SplitHostPort(a.String())
	if err != nil {
		return a.String()
	}
	return host
}

func (h *Hub) handle(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil {
		http.Error(w, "TLS required", http.StatusBadRequest)
		return
	}
	key, err := agentproto.PeerKey(*r.TLS)
	if err != nil {
		http.Error(w, "client key required", http.StatusBadRequest)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	// Until welcome only a small hello may come (revision 2.3 no. 13).
	conn.SetReadLimit(agentproto.MaxHelloMessage)
	remote := hostOf(stringAddr(r.RemoteAddr))
	since, ok := r.Context().Value(handshakeDone{}).(time.Time)
	if !ok {
		since = time.Now()
	}
	h.session(r.Context(), conn, key, remote, since.Add(h.hello))
}

type stringAddr string

func (a stringAddr) Network() string { return "tcp" }
func (a stringAddr) String() string  { return string(a) }

// session admits the agent with its hello and then serves it until the
// connection ends.
func (h *Hub) session(ctx context.Context, conn *websocket.Conn, key ed25519.PublicKey, remote string, helloBy time.Time) {
	fp := keys.Fingerprint(key)
	attrs := []any{slog.String("remote", remote), slog.String("fingerprint", fp)}
	hctx, cancel := context.WithDeadline(ctx, helloBy)
	_, data, err := conn.Read(hctx)
	late := hctx.Err() != nil
	cancel()
	if err != nil {
		if late && ctx.Err() == nil {
			h.log.Warn("no hello in time", append(attrs, logging.Event(logging.AgentDisconnected),
				slog.String(logging.KeyReason, ReasonTimeout))...)
			_ = conn.Close(websocket.StatusPolicyViolation, "no hello in time")
			return
		}
		h.closeRead(conn, err, attrs)
		return
	}
	e, known, err := agentproto.Decode(data, agentproto.MaxAgentMessage, agentproto.FromAgent)
	if err != nil || !known || e.Type != agentproto.TypeHello {
		h.reject(ctx, conn, "", agentproto.RejectInvalid, "the first message must be hello", attrs)
		return
	}
	hello, err := agentproto.Payload[agentproto.Hello](e)
	if err == nil {
		err = hello.Clean()
	}
	if err != nil {
		h.reject(ctx, conn, e.ID, agentproto.RejectInvalid, "invalid hello", attrs)
		return
	}
	attrs = append(attrs, slog.String("agent_name", hello.Name), slog.String(logging.KeyVersion, hello.Version))
	if !agentproto.SameMajor(hello.Version, buildinfo.Version) {
		h.reject(ctx, conn, e.ID, agentproto.RejectVersion,
			"agent "+hello.Version+" and panel "+buildinfo.Version+" differ in the major version", attrs)
		return
	}
	adm, err := h.registry.Admit(ctx, hello, key, remote)
	if err != nil {
		h.log.Error("agent admission failed", append(attrs, slog.Any("err", err))...)
		_ = conn.Close(websocket.StatusInternalError, "admission failed")
		return
	}
	if adm.PinMismatch {
		h.log.Warn("agent key differs from the pinned key", append(attrs, logging.Event(logging.AgentRejected),
			slog.String(logging.KeyReason, "pin_mismatch"), slog.String("agent_id", hello.AgentID))...)
		h.send(ctx, conn, agentproto.TypeReject, e.ID, agentproto.Reject{Reason: agentproto.RejectUnknown})
		_ = conn.Close(websocket.StatusPolicyViolation, "pin mismatch")
		return
	}
	if adm.Reject != "" {
		h.reject(ctx, conn, e.ID, adm.Reject, "", attrs)
		return
	}
	s := newSession(h, conn, adm.AgentID, hello, remote)
	if adm.Paired {
		h.log.Info("agent paired", append(attrs, logging.Event(logging.AgentPaired), slog.String("agent_id", adm.AgentID))...)
	}
	conn.SetReadLimit(agentproto.MaxAgentMessage)
	h.register(s)
	defer h.unregister(s)
	if !h.send(ctx, conn, agentproto.TypeWelcome, e.ID, agentproto.Welcome{AgentID: adm.AgentID}) {
		return
	}
	h.log.Info("agent connected", append(attrs, logging.Event(logging.AgentConnected), slog.String("agent_id", adm.AgentID),
		slog.Any("capabilities", hello.Capabilities))...)
	reason := s.run(ctx)
	h.log.Info("agent disconnected", append(attrs, logging.Event(logging.AgentDisconnected),
		slog.String("agent_id", adm.AgentID), slog.String(logging.KeyReason, reason))...)
}

// closeRead handles a failed read before admission.
func (h *Hub) closeRead(conn *websocket.Conn, err error, attrs []any) {
	if websocket.CloseStatus(err) == websocket.StatusMessageTooBig || strings.Contains(err.Error(), "read limited") {
		h.log.Warn("agent message over the size limit", append(attrs, logging.Event(logging.AgentLimit),
			slog.String(logging.KeyReason, "message_size"))...)
		_ = conn.Close(websocket.StatusMessageTooBig, "message too large")
		return
	}
	_ = conn.Close(websocket.StatusPolicyViolation, "no hello")
}

func (h *Hub) reject(ctx context.Context, conn *websocket.Conn, ref, reason, msg string, attrs []any) {
	h.log.Warn("agent rejected", append(attrs, logging.Event(logging.AgentRejected), slog.String(logging.KeyReason, reason))...)
	h.send(ctx, conn, agentproto.TypeReject, ref, agentproto.Reject{Reason: reason, Message: msg})
	_ = conn.Close(websocket.StatusPolicyViolation, reason)
}

// send writes one message; false means the connection is gone.
func (h *Hub) send(ctx context.Context, conn *websocket.Conn, typ, ref string, payload any) bool {
	data, err := agentproto.Encode(typ, ref, payload)
	if err != nil {
		return false
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, data) == nil
}

func (h *Hub) register(s *Session) {
	h.mu.Lock()
	old := h.sessions[s.AgentID]
	h.sessions[s.AgentID] = s
	h.mu.Unlock()
	if old != nil {
		// A reconnect replaces the older session of the same agent; the
		// close handshake must not hold up the new session.
		go old.close(websocket.StatusPolicyViolation, "replaced")
	}
}

func (h *Hub) unregister(s *Session) {
	h.mu.Lock()
	last := h.sessions[s.AgentID] == s
	if last {
		delete(h.sessions, s.AgentID)
	}
	h.mu.Unlock()
	if last && h.onDisconnect != nil {
		h.onDisconnect(s.AgentID)
	}
}

// Revoke tells a connected agent that its pairing is revoked and ends
// its session (Design 1.7).
func (h *Hub) Revoke(agentID string) {
	h.mu.Lock()
	s := h.sessions[agentID]
	h.mu.Unlock()
	if s == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.Send(ctx, agentproto.TypeRevoked, "", struct{}{})
	go s.close(websocket.StatusPolicyViolation, ReasonRevoked)
}

// Connected reports whether an agent has a session.
func (h *Hub) Connected(agentID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.sessions[agentID]
	return ok
}

func (h *Hub) closeAll() {
	h.mu.Lock()
	all := make([]*Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		all = append(all, s)
	}
	h.mu.Unlock()
	for _, s := range all {
		s.close(websocket.StatusGoingAway, "panel stops")
	}
}
