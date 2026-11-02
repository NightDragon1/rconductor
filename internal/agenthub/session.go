// SPDX-License-Identifier: AGPL-3.0-or-later

package agenthub

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/NightDragon1/rconductor/internal/agentproto"
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

	mu     sync.Mutex
	reason string // why the session ends, set by close
}

func newSession(h *Hub, conn *websocket.Conn, agentID string, hello agentproto.Hello, remote string) *Session {
	return &Session{AgentID: agentID, Hello: hello, Remote: remote, h: h, conn: conn, cancel: func() {}}
}

// run reads messages until the connection ends and returns the reason.
func (s *Session) run(ctx context.Context) string {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()
	defer cancel()
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

// dispatch handles a message of an admitted agent; the cache and the
// orders follow in later steps.
func (h *Hub) dispatch(s *Session, e agentproto.Envelope) {
	h.log.Debug("agent message", slog.String("agent_id", s.AgentID), slog.String("type", e.Type))
}
