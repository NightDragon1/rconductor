// SPDX-License-Identifier: AGPL-3.0-or-later

// Package events is the event bus (Architecture 4): it distributes live
// changes to the WebSockets of the browsers, filtered by rights (Design
// 2.3). Rights are checked on subscribe and again after every change of
// rights; withdrawn subscriptions end with an error. Ended sessions close
// their sockets (AUTH-006). Connections and subscriptions per session are
// limited (SEC-008).
package events

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/NightDragon1/rconductor/internal/auth"
	"github.com/NightDragon1/rconductor/internal/commands"
	"github.com/NightDragon1/rconductor/internal/connmgr"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/perm"
)

// Limits of the WebSocket per session (SEC-008, revision 2.2 candidate 10).
type Limits struct {
	ConnsPerSession int
	SubsPerSession  int
}

// DefaultLimits are 4 connections and 32 subscriptions per session.
var DefaultLimits = Limits{ConnsPerSession: 4, SubsPerSession: 32}

// Timing and sizes of a connection.
const (
	PingInterval = 30 * time.Second // Design 2.3
	pongTimeout  = 30 * time.Second // no pong 60 s after the last one: gone
	maxMessage   = 4 << 10
	sendBuffer   = 64
)

// Reasons sent with error messages.
const (
	ReasonNotFound    = "not_found"
	ReasonUnsupported = "unsupported"
	ReasonInvalid     = "invalid"
	ReasonLimit       = "limit"
	ReasonRevoked     = "revoked"
)

// Viewer is told while someone views a server, so that its runtime data
// stays fresh (Overview 4.6); the cache implements it.
type Viewer interface {
	Watch(ctx context.Context, serverID string) func()
}

// Sessions tells whether a session may still be used.
type Sessions interface {
	SessionValid(ctx context.Context, hash string) bool
}

type message struct {
	Type   string `json:"type"`
	Topic  string `json:"topic,omitempty"`
	Data   any    `json:"data,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type subscription struct {
	topic   string
	server  string // empty for servers.overview
	kind    string // overview, status or console
	release func()
}

type client struct {
	conn      *websocket.Conn
	p         auth.Principal
	requestID string
	out       chan []byte
	cancel    context.CancelFunc
	subs      map[string]*subscription
}

// Hub holds the open sockets.
type Hub struct {
	perm     *perm.Checker
	viewer   Viewer
	sessions Sessions
	log      *slog.Logger
	limits   Limits
	ping     time.Duration

	mu        sync.Mutex
	clients   map[*client]bool
	bySession map[string]map[*client]bool
}

// NewHub returns the event bus; it checks subscriptions again after every
// change of rights.
func NewHub(c *perm.Checker, viewer Viewer, sessions Sessions, log *slog.Logger, limits Limits) *Hub {
	h := &Hub{
		perm: c, viewer: viewer, sessions: sessions, log: log, limits: limits, ping: PingInterval,
		clients: map[*client]bool{}, bySession: map[string]map[*client]bool{},
	}
	c.OnInvalidate(h.Recheck)
	return h
}

func (h *Hub) attrs(c *client) []any {
	return []any{
		slog.String(logging.KeyRequestID, c.requestID), slog.String(logging.KeySession, c.p.SessionRef()),
		slog.String(logging.KeyUser, c.p.Pseudonym), slog.String(logging.KeyRole, c.p.Role),
	}
}

func (c *client) send(m message) bool {
	b, err := json.Marshal(m)
	if err != nil {
		return false
	}
	select {
	case c.out <- b:
		return true
	default:
		// A browser that does not read is disconnected rather than
		// buffered without limit.
		c.cancel()
		return false
	}
}

// Serve runs one socket of an authenticated session until it closes.
func (h *Hub) Serve(ctx context.Context, conn *websocket.Conn, p auth.Principal, requestID string) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	c := &client{conn: conn, p: p, requestID: requestID, out: make(chan []byte, sendBuffer), cancel: cancel, subs: map[string]*subscription{}}
	conn.SetReadLimit(maxMessage)

	h.mu.Lock()
	if len(h.bySession[p.SessionHash]) >= h.limits.ConnsPerSession {
		h.mu.Unlock()
		h.log.Info("websocket refused", append(h.attrs(c), logging.Event(logging.WSSubscribeDenied), slog.String(logging.KeyReason, ReasonLimit))...)
		_ = conn.Close(websocket.StatusPolicyViolation, "too many connections")
		return
	}
	h.clients[c] = true
	if h.bySession[p.SessionHash] == nil {
		h.bySession[p.SessionHash] = map[*client]bool{}
	}
	h.bySession[p.SessionHash][c] = true
	h.mu.Unlock()
	defer h.remove(c)

	go h.writeLoop(ctx, c)
	go h.pingLoop(ctx, c)
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var m message
		if json.Unmarshal(data, &m) != nil {
			c.send(message{Type: "error", Reason: ReasonInvalid})
			continue
		}
		switch m.Type {
		case "subscribe":
			h.subscribe(ctx, c, m.Topic)
		case "unsubscribe":
			h.unsubscribe(c, m.Topic)
		default:
			c.send(message{Type: "error", Topic: m.Topic, Reason: ReasonInvalid})
		}
	}
}

func (h *Hub) remove(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	if s := h.bySession[c.p.SessionHash]; s != nil {
		delete(s, c)
		if len(s) == 0 {
			delete(h.bySession, c.p.SessionHash)
		}
	}
	subs := c.subs
	c.subs = map[string]*subscription{}
	h.mu.Unlock()
	for _, s := range subs {
		if s.release != nil {
			s.release()
		}
	}
	_ = c.conn.CloseNow()
}

func (h *Hub) writeLoop(ctx context.Context, c *client) {
	for {
		select {
		case <-ctx.Done():
			_ = c.conn.Close(websocket.StatusGoingAway, "closed")
			return
		case b := <-c.out:
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.conn.Write(wctx, websocket.MessageText, b)
			cancel()
			if err != nil {
				c.cancel()
				return
			}
		}
	}
}

// pingLoop keeps the connection alive and ends it when the browser stops
// answering or the session is no longer valid.
func (h *Hub) pingLoop(ctx context.Context, c *client) {
	t := time.NewTicker(h.ping)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if h.sessions != nil && !h.sessions.SessionValid(ctx, c.p.SessionHash) {
				_ = c.conn.Close(websocket.StatusPolicyViolation, "session ended")
				c.cancel()
				return
			}
			pctx, cancel := context.WithTimeout(ctx, pongTimeout)
			err := c.conn.Ping(pctx)
			cancel()
			if err != nil {
				c.cancel()
				return
			}
		}
	}
}

// parseTopic splits server.<id>.<kind> and servers.overview (Design 2.3).
func parseTopic(topic string) (server, kind string, err error) {
	if topic == "servers.overview" {
		return "", "overview", nil
	}
	parts := strings.Split(topic, ".")
	if len(parts) != 3 || parts[0] != "server" || parts[1] == "" {
		return "", "", errors.New(ReasonInvalid)
	}
	switch parts[2] {
	case "status", "console":
		return parts[1], parts[2], nil
	case "metrics", "logs":
		// Agent data follows with M3.
		return "", "", errors.New(ReasonUnsupported)
	}
	return "", "", errors.New(ReasonInvalid)
}

func (h *Hub) deny(c *client, topic, reason string) {
	h.log.Info("subscription refused", append(h.attrs(c), logging.Event(logging.WSSubscribeDenied),
		slog.String("topic", topic), slog.String(logging.KeyReason, reason))...)
	c.send(message{Type: "error", Topic: topic, Reason: reason})
}

func (h *Hub) subscribe(ctx context.Context, c *client, topic string) {
	server, kind, err := parseTopic(topic)
	if err != nil {
		h.deny(c, topic, err.Error())
		return
	}
	if server != "" {
		rights, err := h.perm.For(ctx, c.p.UserID)
		if err != nil {
			h.deny(c, topic, ReasonInvalid)
			return
		}
		if _, ok := rights.Server(server); !ok {
			h.deny(c, topic, ReasonNotFound)
			return
		}
	}
	s := &subscription{topic: topic, server: server, kind: kind}
	if server != "" && h.viewer != nil {
		// Registered before the subscription is visible, so a recheck
		// that ends it always finds the release.
		s.release = h.viewer.Watch(ctx, server)
	}
	h.mu.Lock()
	_, exists := c.subs[topic]
	n := 0
	for other := range h.bySession[c.p.SessionHash] {
		n += len(other.subs)
	}
	if !exists && n < h.limits.SubsPerSession {
		c.subs[topic] = s
	}
	h.mu.Unlock()
	if exists || n >= h.limits.SubsPerSession {
		if s.release != nil {
			s.release()
		}
		if !exists {
			h.deny(c, topic, ReasonLimit)
		}
		return
	}
	h.log.Info("subscribed", append(h.attrs(c), logging.Event(logging.WSSubscribe), slog.String("topic", topic))...)
}

func (h *Hub) unsubscribe(c *client, topic string) {
	h.mu.Lock()
	s := c.subs[topic]
	delete(c.subs, topic)
	h.mu.Unlock()
	if s != nil && s.release != nil {
		s.release()
	}
}

// Recheck evaluates every subscription again after a change of rights;
// subscriptions to servers that left the scope end with an error, also
// when the session stays (revision 2.2 candidate 4).
func (h *Hub) Recheck() {
	ctx := context.Background()
	h.mu.Lock()
	clients := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()
	for _, c := range clients {
		rights, err := h.perm.For(ctx, c.p.UserID)
		if err != nil {
			continue
		}
		var gone []*subscription
		h.mu.Lock()
		for topic, s := range c.subs {
			if s.server == "" {
				continue
			}
			if _, ok := rights.Server(s.server); !ok {
				delete(c.subs, topic)
				gone = append(gone, s)
			}
		}
		h.mu.Unlock()
		for _, s := range gone {
			if s.release != nil {
				s.release()
			}
			h.log.Info("subscription withdrawn", append(h.attrs(c), slog.String("topic", s.topic), slog.String(logging.KeyReason, ReasonRevoked))...)
			c.send(message{Type: "error", Topic: s.topic, Reason: ReasonRevoked})
		}
	}
}

// CloseSessions closes the sockets of ended sessions at once (AUTH-006).
func (h *Hub) CloseSessions(hashes []string) {
	h.mu.Lock()
	var victims []*client
	for _, hash := range hashes {
		for c := range h.bySession[hash] {
			victims = append(victims, c)
		}
	}
	h.mu.Unlock()
	for _, c := range victims {
		_ = c.conn.Close(websocket.StatusPolicyViolation, "session ended")
		c.cancel()
	}
}

// each calls f for every subscription of kind on server (or overview).
func (h *Hub) each(kind, server string, f func(c *client, s *subscription)) {
	h.mu.Lock()
	type pair struct {
		c *client
		s *subscription
	}
	var targets []pair
	for c := range h.clients {
		for _, s := range c.subs {
			if s.kind == kind && s.server == server {
				targets = append(targets, pair{c, s})
			}
		}
	}
	h.mu.Unlock()
	for _, t := range targets {
		f(t.c, t.s)
	}
}

// StatusEvent is the data of a status change.
type StatusEvent struct {
	ServerID string    `json:"server_id"`
	Status   string    `json:"status"`
	Since    time.Time `json:"since"`
}

// PublishStatus sends a reachability change to server.<id>.status and to
// servers.overview of everyone who has the server in scope.
func (h *Hub) PublishStatus(serverID string, status connmgr.Status, since time.Time) {
	ev := StatusEvent{ServerID: serverID, Status: string(status), Since: since}
	h.each("status", serverID, func(c *client, s *subscription) {
		c.send(message{Type: "event", Topic: s.topic, Data: ev})
	})
	h.each("overview", "", func(c *client, s *subscription) {
		rights, err := h.perm.For(context.Background(), c.p.UserID)
		if err != nil {
			return
		}
		if _, ok := rights.Server(serverID); ok {
			c.send(message{Type: "event", Topic: s.topic, Data: ev})
		}
	})
}

// ConsoleEvent is a command block of the live console (CMD-012, CMD-014).
type ConsoleEvent struct {
	RequestID string    `json:"request_id"`
	TS        time.Time `json:"ts"`
	UserID    string    `json:"user_id"`
	Pseudonym string    `json:"pseudonym"`
	Role      string    `json:"role"`
	Command   string    `json:"command"`
	Response  string    `json:"response,omitempty"`
	Truncated bool      `json:"truncated,omitempty"`
	Result    string    `json:"result"`
	Reason    string    `json:"reason,omitempty"`
}

// PublishConsole sends a finished or refused command to server.<id>.console.
// RCON admins and server admins see all commands; moderators and supporters
// only their own in R1 (Design 6.3).
func (h *Hub) PublishConsole(serverID string, e commands.Entry) {
	ev := ConsoleEvent{
		RequestID: e.RequestID, TS: e.TS, UserID: e.UserID, Pseudonym: e.Pseudonym, Role: e.Role,
		Command: e.Command, Response: e.Response, Truncated: e.Truncated, Result: e.Result, Reason: e.Reason,
	}
	h.each("console", serverID, func(c *client, s *subscription) {
		role := perm.Role(c.p.Role)
		if role != perm.RCONAdmin && role != perm.ServerAdmin && e.UserID != c.p.UserID {
			return
		}
		c.send(message{Type: "event", Topic: s.topic, Data: ev})
	})
}
