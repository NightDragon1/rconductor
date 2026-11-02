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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/NightDragon1/rconductor/internal/auth"
	"github.com/NightDragon1/rconductor/internal/commands"
	"github.com/NightDragon1/rconductor/internal/connmgr"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Limits of the WebSocket (SEC-008, revision 2.2 candidate 10): sockets
// per session and subscriptions per socket.
type Limits struct {
	SocketsPerSession int
	SubsPerSocket     int
}

// LimitsFunc returns the limits in force; they are read again for every
// new socket and subscription, so a changed setting applies at once.
type LimitsFunc func(ctx context.Context) Limits

// SettingsLimits reads the limits from the global settings
// ws.max_sockets_per_session and ws.max_subscriptions_per_socket, which
// only the RCON admin changes.
func SettingsLimits(st *store.Store) LimitsFunc {
	return func(ctx context.Context) Limits {
		l := Limits{SocketsPerSession: users.DefaultWSMaxSockets, SubsPerSocket: users.DefaultWSMaxSubs}
		var n int
		if err := st.GlobalSetting(ctx, users.SettingWSMaxSockets, &n); err == nil && n > 0 {
			l.SocketsPerSession = n
		}
		if err := st.GlobalSetting(ctx, users.SettingWSMaxSubs, &n); err == nil && n > 0 {
			l.SubsPerSocket = n
		}
		return l
	}
}

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
	ReasonReplaced    = "replaced"
	ReasonForbidden   = "forbidden"
)

// Viewer is told while someone views a server, so that its runtime data
// stays fresh (Overview 4.6); the cache implements it.
type Viewer interface {
	Watch(ctx context.Context, serverID string) func()
}

// AgentViews provides the agent data of a server: the current value for a
// new subscription, and live logs while someone follows them (UI-009).
type AgentViews interface {
	// Current returns the last metrics or log lines of the server.
	Current(ctx context.Context, serverID, kind string) (any, bool)
	// FollowLogs keeps the logs of the server streaming until release.
	FollowLogs(ctx context.Context, serverID string) (release func())
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
	opened    time.Time
	conn      *websocket.Conn
	p         auth.Principal
	requestID string
	out       chan []byte
	cancel    context.CancelFunc
	subs      map[string]*subscription
}

// Hub holds the open sockets.
type Hub struct {
	perm   *perm.Checker
	viewer Viewer
	// agentViews serves server.<id>.metrics and .logs; nil until set.
	agentViews AgentViews
	sessions   Sessions
	log        *slog.Logger
	limits     LimitsFunc
	ping       time.Duration

	mu        sync.Mutex
	clients   map[*client]bool
	bySession map[string]map[*client]bool
}

// SetAgentViews connects the agent data for the metrics and logs topics.
func (h *Hub) SetAgentViews(v AgentViews) { h.agentViews = v }

// NewHub returns the event bus; it checks subscriptions again after every
// change of rights.
func NewHub(c *perm.Checker, viewer Viewer, sessions Sessions, log *slog.Logger, limits LimitsFunc) *Hub {
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
	c := &client{opened: time.Now(), conn: conn, p: p, requestID: requestID, out: make(chan []byte, sendBuffer), cancel: cancel, subs: map[string]*subscription{}}
	conn.SetReadLimit(maxMessage)
	limit := h.limits(ctx).SocketsPerSession

	h.mu.Lock()
	// Over the limit the oldest sockets of the session give way to the
	// new one, so a reloaded browser tab never locks itself out.
	var replaced []*client
	for len(h.bySession[p.SessionHash])-len(replaced) >= limit {
		var oldest *client
		for other := range h.bySession[p.SessionHash] {
			if !slices.Contains(replaced, other) && (oldest == nil || other.opened.Before(oldest.opened)) {
				oldest = other
			}
		}
		if oldest == nil {
			break
		}
		replaced = append(replaced, oldest)
	}
	h.clients[c] = true
	if h.bySession[p.SessionHash] == nil {
		h.bySession[p.SessionHash] = map[*client]bool{}
	}
	h.bySession[p.SessionHash][c] = true
	h.mu.Unlock()
	defer h.remove(c)
	// A session that ended between the upgrade and the registration above
	// missed CloseSessions; check once more now that it would be reached.
	if h.sessions != nil && !h.sessions.SessionValid(ctx, p.SessionHash) {
		_ = conn.Close(websocket.StatusPolicyViolation, "session ended")
		return
	}
	for _, old := range replaced {
		h.log.Info("websocket replaced by a newer one", append(h.attrs(old), slog.String(logging.KeyReason, ReasonReplaced))...)
		h.closeWith(old, ReasonReplaced)
	}

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
		return parts[1], parts[2], nil
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
		sr, ok := rights.Server(server)
		if !ok {
			h.deny(c, topic, ReasonNotFound)
			return
		}
		// Metrics and logs are agent functions: RCON admin and server
		// admin only (AGT-008, T-014).
		if agentKind(kind) && !sr.Agent {
			h.deny(c, topic, ReasonForbidden)
			return
		}
		if agentKind(kind) && h.agentViews == nil {
			h.deny(c, topic, ReasonUnsupported)
			return
		}
	}
	s := &subscription{topic: topic, server: server, kind: kind}
	switch {
	case kind == "logs":
		// Registered before the subscription is visible, so a recheck
		// that ends it always finds the release.
		s.release = h.agentViews.FollowLogs(ctx, server)
	case kind == "metrics":
	case server != "" && h.viewer != nil:
		s.release = h.viewer.Watch(ctx, server)
	}
	limit := h.limits(ctx).SubsPerSocket
	h.mu.Lock()
	_, exists := c.subs[topic]
	n := len(c.subs)
	if !exists && n < limit {
		c.subs[topic] = s
	}
	h.mu.Unlock()
	if exists || n >= limit {
		if s.release != nil {
			s.release()
		}
		if !exists {
			h.deny(c, topic, ReasonLimit)
		}
		return
	}
	h.log.Info("subscribed", append(h.attrs(c), logging.Event(logging.WSSubscribe), slog.String("topic", topic))...)
	if agentKind(kind) {
		if v, ok := h.agentViews.Current(ctx, server, kind); ok {
			c.send(message{Type: "event", Topic: topic, Data: v})
		}
	}
}

func agentKind(kind string) bool { return kind == "metrics" || kind == "logs" }

// PublishAgentData sends metrics or log lines of a server to the
// subscribers of server.<id>.metrics or .logs.
func (h *Hub) PublishAgentData(serverID, kind string, data any) {
	h.each(kind, serverID, func(c *client, s *subscription) {
		c.send(message{Type: "event", Topic: s.topic, Data: data})
	})
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
			if sr, ok := rights.Server(s.server); !ok || (agentKind(s.kind) && !sr.Agent) {
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

// closeWith sends an error with reason and closes the socket; the message
// is written before the close frame.
func (h *Hub) closeWith(c *client, reason string) {
	b, _ := json.Marshal(message{Type: "error", Reason: reason})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.conn.Write(ctx, websocket.MessageText, b)
	_ = c.conn.Close(websocket.StatusPolicyViolation, reason)
	c.cancel()
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
