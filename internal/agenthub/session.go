// SPDX-License-Identifier: AGPL-3.0-or-later

package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/foreign"
	"github.com/NightDragon1/rconductor/internal/logging"
)

// Session is the connection of one admitted agent.
type Session struct {
	AgentID string
	Hello   agentproto.Hello
	Remote  string

	h      *Hub
	conn   *websocket.Conn
	cancel context.CancelFunc

	mu      sync.Mutex
	reason  string // why the session ends, set by close
	pending map[string]chan agentproto.Result
	// collect takes the messages that answer an order with data before
	// its result (logs.tail), by order ID.
	collect map[string]func(agentproto.Envelope)
	done    chan struct{} // closed when run ends
}

func newSession(h *Hub, conn *websocket.Conn, agentID string, hello agentproto.Hello, remote string) *Session {
	return &Session{AgentID: agentID, Hello: hello, Remote: remote, h: h, conn: conn, cancel: func() {}, pending: map[string]chan agentproto.Result{}, collect: map[string]func(agentproto.Envelope){}, done: make(chan struct{})}
}

// run reads messages until the connection ends and returns the reason.
func (s *Session) run(ctx context.Context) string {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	defer cancel()
	defer close(s.done)
	go s.pingLoop(ctx)
	for {
		_, data, err := s.conn.Read(ctx)
		if err != nil {
			return s.endReason(err)
		}
		e, known, err := agentproto.Decode(data, agentproto.MaxAgentMessage, agentproto.FromAgent)
		if err != nil || !known || e.Type == agentproto.TypeHello {
			s.h.log.Warn("agent broke the protocol", slog.String("agent_id", s.AgentID), slog.String("type", e.Type))
			s.close(websocket.StatusPolicyViolation, ReasonProtocol)
			return ReasonProtocol
		}
		s.h.dispatch(s, e)
	}
}

func (s *Session) endReason(err error) string {
	s.mu.Lock()
	r := s.reason
	s.mu.Unlock()
	switch {
	case r != "":
		return r
	case websocket.CloseStatus(err) == websocket.StatusMessageTooBig || strings.Contains(err.Error(), "read limited"):
		s.h.log.Warn("agent message over the size limit", logging.Event(logging.AgentLimit),
			slog.String("agent_id", s.AgentID), slog.String(logging.KeyReason, "message_size"))
		_ = s.conn.Close(websocket.StatusMessageTooBig, "message too large")
		return ReasonTooLarge
	case errors.Is(err, context.DeadlineExceeded):
		return ReasonTimeout
	}
	return ReasonClosed
}

// pingLoop pings every 30 s; without a pong within another 30 s the
// session ends (Design 1.1: gone after 60 s).
func (s *Session) pingLoop(ctx context.Context) {
	t := time.NewTicker(s.h.ping)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, pongTimeout)
			err := s.conn.Ping(pctx)
			cancel()
			if err != nil && ctx.Err() == nil {
				s.close(websocket.StatusPolicyViolation, ReasonTimeout)
				return
			}
		}
	}
}

// Send writes a message to the agent.
func (s *Session) Send(ctx context.Context, typ, ref string, payload any) error {
	data, err := agentproto.Encode(typ, ref, payload)
	if err != nil {
		return err
	}
	if len(data) > agentproto.MaxPanelMessage {
		return errors.New("order over the size limit")
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.conn.Write(wctx, websocket.MessageText, data)
}

// close ends the session with a reason for agent.disconnected.
func (s *Session) close(code websocket.StatusCode, reason string) {
	s.mu.Lock()
	if s.reason == "" {
		s.reason = reason
	}
	cancel := s.cancel
	s.mu.Unlock()
	_ = s.conn.Close(code, reason)
	cancel()
}

// request sends an order and waits for its result until ctx ends; collect,
// if set, gets the messages that answer the order before its result.
func (s *Session) request(ctx context.Context, typ string, payload any, collect func(agentproto.Envelope)) (agentproto.Result, error) {
	e, err := agentproto.New(typ, "", payload)
	if err != nil {
		return agentproto.Result{}, err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return agentproto.Result{}, err
	}
	if len(data) > agentproto.MaxPanelMessage {
		return agentproto.Result{}, errors.New("order over the size limit")
	}
	ch := make(chan agentproto.Result, 1)
	s.mu.Lock()
	if len(s.pending) >= maxPending {
		s.mu.Unlock()
		return agentproto.Result{}, ErrBusy
	}
	s.pending[e.ID] = ch
	if collect != nil {
		s.collect[e.ID] = collect
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, e.ID)
		delete(s.collect, e.ID)
		s.mu.Unlock()
	}()
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = s.conn.Write(wctx, websocket.MessageText, data)
	cancel()
	if err != nil {
		return agentproto.Result{}, ErrNotConnected
	}
	select {
	case r := <-ch:
		return r, nil
	case <-s.done:
		return agentproto.Result{}, ErrNotConnected
	case <-ctx.Done():
		return agentproto.Result{}, ctx.Err()
	}
}

// result hands a result to the order waiting for it; results nobody
// waits for are dropped.
func (s *Session) result(e agentproto.Envelope) {
	r, err := agentproto.Payload[agentproto.Result](e)
	if err != nil {
		return
	}
	r.Code = foreign.Name(r.Code, 32)
	r.Message = foreign.Name(r.Message, 512)
	s.mu.Lock()
	ch := s.pending[e.Ref]
	s.mu.Unlock()
	if ch != nil {
		select {
		case ch <- r:
		default:
		}
	}
}

// dispatch handles a message of an admitted agent.
func (h *Hub) dispatch(s *Session, e agentproto.Envelope) {
	if e.Type != agentproto.TypeResult && e.Ref != "" {
		s.mu.Lock()
		c := s.collect[e.Ref]
		s.mu.Unlock()
		if c != nil {
			// An answer with data goes to its order, not to the live data.
			c(e)
			return
		}
	}
	switch e.Type {
	case agentproto.TypeResult:
		s.result(e)
	default:
		if h.onMessage != nil {
			h.onMessage(s.AgentID, e)
		}
	}
}
