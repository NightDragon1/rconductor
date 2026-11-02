// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agent is the host agent (Design 1): it connects to the panel's
// agent port with TLS 1.3 and pinned keys, never opens a port itself
// (AGT-011) and carries out only the operations of Design 1.5 through its
// backend. Everything the panel sends is checked again here (Design 1.6).
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/buildinfo"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/ops/keys"
)

// Sender sends a message to the panel; it is safe for concurrent use.
type Sender interface {
	Send(ctx context.Context, typ, ref string, payload any) error
}

// Backend carries out the work on the host (Docker in R1).
type Backend interface {
	// Name is the backend reported in hello, e.g. docker.
	Name() string
	// Capabilities lists what the backend can do (Design 1.4).
	Capabilities() []string
	// Run sends the snapshot and then events and metrics until ctx ends.
	Run(ctx context.Context, out Sender) error
	// Handle carries out one order of the panel; it answers through out.
	Handle(ctx context.Context, e agentproto.Envelope, out Sender)
}

// Backoff of the reconnect (Design 1.1).
const (
	backoffStart  = time.Second
	backoffFactor = 1.6
	backoffMax    = 120 * time.Second
	dialTimeout   = 15 * time.Second
)

// ErrNoPanel means neither a token nor a panel address is configured.
var ErrNoPanel = errors.New("set RCD_PAIRING_TOKEN (or _FILE) or RCD_PANEL_ADDR")

// Agent is a running agent.
type Agent struct {
	cfg     config.Agent
	log     *slog.Logger
	backend Backend
	id      *keys.Identity

	mu    sync.Mutex
	state State
	// backoff overrides the reconnect waits, for tests.
	backoff func(attempt int) time.Duration
}

// New loads the agent key from the secrets directory (created on first
// start) and the pairing state.
func New(cfg config.Agent, log *slog.Logger, backend Backend) (*Agent, error) {
	id, err := keys.LoadOrCreateIdentity(log, filepath.Join(cfg.SecretsDir, keys.AgentKeyFile))
	if err != nil {
		return nil, err
	}
	st, err := loadState(cfg.SecretsDir)
	if err != nil {
		return nil, err
	}
	return &Agent{cfg: cfg, log: log, backend: backend, id: id, state: st, backoff: backoff}, nil
}

// State returns the current pairing state.
func (a *Agent) State() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

func (a *Agent) setState(st State) error {
	if err := st.save(a.cfg.SecretsDir); err != nil {
		return err
	}
	a.mu.Lock()
	a.state = st
	a.mu.Unlock()
	return nil
}

// Fingerprint is the fingerprint of the agent key.
func (a *Agent) Fingerprint() string { return a.id.Fingerprint() }

func backoff(attempt int) time.Duration {
	d := float64(backoffStart)
	for range attempt {
		d *= backoffFactor
	}
	d = min(d, float64(backoffMax))
	d *= 0.8 + 0.4*rand.Float64() //nolint:gosec // jitter, not security
	return time.Duration(d)
}

// plan is how the next connection attempt goes.
type plan struct {
	addr, fingerprint string
	hello             agentproto.Hello
	token             *agentproto.PairingToken
}

func (a *Agent) plan() (plan, error) {
	state := a.State()
	if a.cfg.PairingToken != "" {
		tok, err := agentproto.ParsePairingToken(a.cfg.PairingToken)
		if err != nil {
			return plan{}, fmt.Errorf("RCD_PAIRING_TOKEN: %w", err)
		}
		if fmt.Sprintf("%x", agentproto.SecretHash(tok.Secret)) != state.TokenHash {
			addr := tok.Addr
			if a.cfg.PanelAddr != "" {
				addr = a.cfg.PanelAddr
			}
			if addr == "" {
				return plan{}, errors.New("the pairing token has no panel address; set RCD_PANEL_ADDR")
			}
			return plan{addr: addr, fingerprint: tok.Fingerprint, hello: agentproto.Hello{Token: tok.Secret}, token: &tok}, nil
		}
	}
	if state.Revoked {
		return plan{}, errRevoked
	}
	addr := state.PanelAddr
	if a.cfg.PanelAddr != "" {
		addr = a.cfg.PanelAddr
	}
	if state.AgentID != "" {
		return plan{addr: addr, fingerprint: state.PanelFingerprint, hello: agentproto.Hello{AgentID: state.AgentID}}, nil
	}
	if a.cfg.PanelAddr == "" {
		return plan{}, ErrNoPanel
	}
	// Way B: the first contact pins the panel key (trust on first use).
	return plan{addr: addr, fingerprint: state.PanelFingerprint}, nil
}

var errRevoked = errors.New("the pairing was revoked in the panel; pair again with a new token or by fingerprint")

// Run connects and reconnects until ctx ends. A revoked agent stays idle
// and does not reconnect (Design 1.7).
func (a *Agent) Run(ctx context.Context) error {
	attempt := 0
	for {
		p, err := a.plan()
		switch {
		case errors.Is(err, errRevoked):
			a.log.Warn("agent revoked; not reconnecting", logging.Event(logging.AgentRejected),
				slog.String(logging.KeyReason, agentproto.RejectRevoked), slog.String("hint", err.Error()))
			<-ctx.Done()
			return nil
		case err != nil:
			return err
		}
		start := time.Now()
		reason := a.connect(ctx, p)
		if ctx.Err() != nil {
			return nil //nolint:nilerr // shutting down is no error
		}
		if reason == agentproto.RejectRevoked {
			continue // the next plan stays idle
		}
		if time.Since(start) > backoffMax {
			attempt = 0
		}
		wait := a.backoff(attempt)
		attempt++
		a.log.Info("reconnecting to the panel", slog.String(logging.KeyReason, reason), slog.Duration("wait", wait))
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

// connect runs one connection and returns why it ended.
func (a *Agent) connect(ctx context.Context, p plan) string {
	attrs := []any{slog.String("panel", p.addr), slog.String("fingerprint", a.Fingerprint())}
	cfg, err := agentproto.ClientConfig(a.id, p.fingerprint)
	if err != nil {
		a.log.Error("TLS setup failed", append(attrs, slog.Any("err", err))...)
		return "tls"
	}
	dctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	conn, resp, err := websocket.Dial(dctx, "wss://"+p.addr+agentproto.Path, &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: &http.Transport{TLSClientConfig: cfg, Proxy: nil}},
	})
	var panelFP string
	if resp != nil {
		if resp.TLS != nil {
			if pub, err := agentproto.PeerKey(*resp.TLS); err == nil {
				panelFP = keys.Fingerprint(pub)
			}
		}
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}
	if err != nil {
		if errors.Is(err, agentproto.ErrPinMismatch) || strings.Contains(err.Error(), agentproto.ErrPinMismatch.Error()) {
			a.log.Error("the panel shows another key than the pinned one", append(attrs, logging.Event(logging.AgentRejected),
				slog.String(logging.KeyReason, "pin_mismatch"), slog.String("pinned", p.fingerprint))...)
			return "pin_mismatch"
		}
		a.log.Info("panel not reachable", append(attrs, slog.Any("err", err))...)
		return "unreachable"
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(agentproto.MaxPanelMessage)
	attrs = append(attrs, slog.String("panel_fingerprint", panelFP))
	if p.fingerprint == "" {
		// Way B: the first contact pins the panel key at once, so that a
		// later attempt during the wait cannot reach another panel.
		st := a.State()
		st.PanelAddr, st.PanelFingerprint = p.addr, panelFP
		if err := a.setState(st); err != nil {
			a.log.Error("pairing state not saved", slog.Any("err", err))
			return "state"
		}
		// Compare these two with the RCON admin (Design 1.2).
		a.log.Info("pairing by fingerprint: compare both fingerprints with the panel", attrs...)
	}

	h := p.hello
	h.Name = a.name()
	h.Version = buildinfo.Version
	h.Backend = a.backend.Name()
	h.Capabilities = a.backend.Capabilities()
	s := &sender{conn: conn}
	if err := s.Send(ctx, agentproto.TypeHello, "", h); err != nil {
		return "closed"
	}
	rctx, rcancel := context.WithTimeout(ctx, dialTimeout)
	_, data, err := conn.Read(rctx)
	rcancel()
	if err != nil {
		return "closed"
	}
	e, known, err := agentproto.Decode(data, agentproto.MaxPanelMessage, agentproto.FromPanel)
	if err != nil || !known {
		return "protocol"
	}
	switch e.Type {
	case agentproto.TypeReject:
		r, _ := agentproto.Payload[agentproto.Reject](e)
		return a.rejected(r, attrs)
	case agentproto.TypeWelcome:
	default:
		return "protocol"
	}
	w, err := agentproto.Payload[agentproto.Welcome](e)
	if err != nil || w.AgentID == "" {
		return "protocol"
	}
	st := State{AgentID: w.AgentID, PanelAddr: p.addr, PanelFingerprint: panelFP, TokenHash: a.State().TokenHash}
	if p.token != nil {
		st.TokenHash = fmt.Sprintf("%x", agentproto.SecretHash(p.token.Secret))
	}
	if st != a.State() {
		if err := a.setState(st); err != nil {
			a.log.Error("pairing state not saved", slog.Any("err", err))
			return "state"
		}
	}
	a.log.Info("connected to the panel", append(attrs, logging.Event(logging.AgentHandshake), slog.String("agent_id", w.AgentID))...)
	return a.session(ctx, conn, s)
}

func (a *Agent) rejected(r agentproto.Reject, attrs []any) string {
	attrs = append(attrs, logging.Event(logging.AgentRejected), slog.String(logging.KeyReason, r.Reason))
	switch r.Reason {
	case agentproto.RejectVersion:
		a.log.Error("the panel refuses this agent version: "+r.Message, append(attrs, slog.String(logging.KeyVersion, buildinfo.Version))...)
	case agentproto.RejectPending:
		a.log.Info("pairing waits for approval in the panel", attrs...)
	case agentproto.RejectRevoked:
		a.markRevoked()
	default:
		a.log.Warn("the panel rejected the agent", attrs...)
	}
	return r.Reason
}

func (a *Agent) markRevoked() {
	st := a.State()
	st.Revoked = true
	if err := a.setState(st); err != nil {
		a.log.Error("pairing state not saved", slog.Any("err", err))
	}
}

func (a *Agent) name() string {
	if a.cfg.Name != "" {
		return a.cfg.Name
	}
	return hostname()
}

// session serves orders until the connection ends.
func (a *Agent) session(ctx context.Context, conn *websocket.Conn, s *sender) string {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		if err := a.backend.Run(ctx, s); err != nil && ctx.Err() == nil {
			a.log.Error("backend stopped", slog.Any("err", err))
			cancel()
		}
	}()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return "closed"
			}
			a.log.Info("connection to the panel ended", slog.Any("err", err))
			return "closed"
		}
		e, known, err := agentproto.Decode(data, agentproto.MaxPanelMessage, agentproto.FromPanel)
		if err != nil {
			a.log.Warn("the panel broke the protocol", slog.Any("err", err))
			return "protocol"
		}
		switch {
		case !known || e.Type == agentproto.TypeWelcome || e.Type == agentproto.TypeReject:
			// Only the operations of Design 1.5 exist (AGT-004).
			a.log.Warn("order not supported", logging.Event(logging.AgentUnsupported), slog.String("type", e.Type))
			_ = s.Send(ctx, agentproto.TypeResult, e.ID, agentproto.Result{Code: agentproto.CodeUnsupported})
		case e.Type == agentproto.TypeRevoked:
			a.log.Warn("the panel revoked this agent", logging.Event(logging.AgentRejected), slog.String(logging.KeyReason, agentproto.RejectRevoked))
			a.markRevoked()
			return agentproto.RejectRevoked
		default:
			go a.backend.Handle(ctx, e, s)
		}
	}
}

type sender struct{ conn *websocket.Conn }

func (s *sender) Send(ctx context.Context, typ, ref string, payload any) error {
	data, err := agentproto.Encode(typ, ref, payload)
	if err != nil {
		return err
	}
	if len(data) > agentproto.MaxAgentMessage {
		return fmt.Errorf("message of %d bytes over the limit", len(data))
	}
	wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return s.conn.Write(wctx, websocket.MessageText, data)
}
