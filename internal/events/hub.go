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
	kind    string // overview, rights, status or console
	release func()
	// scope is the servers of the user when last looked at, for overview
	// and rights; under Hub.mu.
	scope []string
	// Until ready, events wait in pending: none may come before subscribed
	// and the current state (Design 2.3, issue #99). after is the last
	// delivery the current state holds. All three under Hub.mu.
	ready   bool
	pending []message
	after   uint64
}

// Sequenced data numbers its delivery (log lines); a subscription drops
// deliveries that its current state already holds.
type Sequenced interface{ Sequence() uint64 }

// maxPending bounds the events held for a subscription that is being set
// up.
const maxPending = 64

// deliver sends an event of a subscription, or holds it while the
// subscription is not ready.
func (h *Hub) deliver(c *client, s *subscription, m message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// The target was taken earlier: it may have ended since (C3).
	if c.subs[s.topic] != s {
		return
	}
	if !s.ready {
		if len(s.pending) < maxPending {
			s.pending = append(s.pending, m)
		}
		return
	}
	if !s.holds(m) {
		c.send(m)
	}
}

func (s *subscription) holds(m message) bool {
	q, ok := m.Data.(Sequenced)
	return ok && q.Sequence() <= s.after
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

// parseTopic splits server.<id>.<kind>, servers.overview and user.rights
// (Design 2.3).
func parseTopic(topic string) (server, kind string, err error) {
	switch topic {
	case "servers.overview":
		return "", "overview", nil
	case "user.rights":
		return "", "rights", nil
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
	case kind == "overview" || kind == "rights":
		if rights, err := h.perm.For(ctx, c.p.UserID); err == nil {
			s.scope = rights.ServerIDs()
			if kind == "overview" {
				s.release = h.watch(ctx, s.scope)
			}
		}
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
	// A change of rights since the first check found no subscription to
	// end: now it is visible to Recheck, so one more look closes the gap.
	if server != "" && !h.mayFollow(ctx, c, server, kind) {
		h.unsubscribe(c, topic)
		h.deny(c, topic, ReasonRevoked)
		return
	}
	h.log.Info("subscribed", append(h.attrs(c), logging.Event(logging.WSSubscribe), slog.String("topic", topic))...)
	// The confirmation comes before any event of the topic (Design 2.3,
	// issue #99).
	c.send(message{Type: "subscribed", Topic: topic})
	var current any
	has := false
	if agentKind(kind) {
		current, has = h.agentViews.Current(ctx, server, kind)
	}
	h.mu.Lock()
	if c.subs[topic] != s {
		h.mu.Unlock()
		return // Recheck ended it meanwhile
	}
	if has {
		c.send(message{Type: "event", Topic: topic, Data: current})
		if q, ok := current.(Sequenced); ok {
			s.after = q.Sequence()
		}
	}
	for _, m := range s.pending {
		if !s.holds(m) {
			c.send(m)
		}
	}
	s.pending, s.ready = nil, true
	h.mu.Unlock()
}

// watch views the servers of an overview: their player lists stay fresh
// while it is open (Gesamtkonzept 4.6). The result ends the view.
func (h *Hub) watch(ctx context.Context, ids []string) func() {
	if h.viewer == nil {
		return nil
	}
	releases := make([]func(), 0, len(ids))
	for _, id := range ids {
		releases = append(releases, h.viewer.Watch(ctx, id))
	}
	return func() {
		for _, r := range releases {
			r()
		}
	}
}

// mayFollow checks a topic of a server against the current rights.
func (h *Hub) mayFollow(ctx context.Context, c *client, server, kind string) bool {
	rights, err := h.perm.For(ctx, c.p.UserID)
	if err != nil {
		return false
	}
	sr, ok := rights.Server(server)
	return ok && (!agentKind(kind) || sr.Agent)
}

func agentKind(kind string) bool { return kind == "metrics" || kind == "logs" }

// PublishAgentData sends metrics or log lines of a server to the
// subscribers of server.<id>.metrics or .logs.
func (h *Hub) PublishAgentData(serverID, kind string, data any) {
	h.each(kind, serverID, func(c *client, s *subscription) {
		h.deliver(c, s, message{Type: "event", Topic: s.topic, Data: data})
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
// when the session stays (revision 2.2 candidate 4). When the servers of a
// user changed, the overview views the new ones and user.rights tells the
// interface to load again (D-044).
func (h *Hub) Recheck() {
	ctx := context.Background()
	h.mu.Lock()
	clients := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()
	for _, c := range clients {
		// Rights that cannot be read end what depends on them: standard is
		// refusal, and the client can subscribe again (B1).
		rights, err := h.perm.For(ctx, c.p.UserID)
		var ids []string
		if err == nil {
			ids = rights.ServerIDs()
		}
		var gone, moved []*subscription
		h.mu.Lock()
		for topic, s := range c.subs {
			if s.server == "" {
				if err == nil && !slices.Equal(s.scope, ids) {
					moved = append(moved, s)
				}
				continue
			}
			if err != nil {
				delete(c.subs, topic)
				gone = append(gone, s)
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
		for _, s := range moved {
			h.follow(ctx, c, s, ids)
		}
	}
}

// follow brings a subscription without a server to the new servers of its
// user; the views of the overview change before the old ones end, so that
// a server in both stays viewed.
func (h *Hub) follow(ctx context.Context, c *client, s *subscription, ids []string) {
	var view func()
	if s.kind == "overview" {
		view = h.watch(ctx, ids)
	}
	h.mu.Lock()
	active := c.subs[s.topic] == s
	old := s.release
	if active {
		s.scope = ids
		if s.kind == "overview" {
			s.release = view
		}
	}
	h.mu.Unlock()
	switch {
	case !active:
		if view != nil {
			view()
		}
	case s.kind == "rights":
		h.deliver(c, s, message{Type: "event", Topic: s.topic})
	case old != nil:
		old()
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

// StatusEvent is the data of a status change; in servers.overview its
// kind is status, next to runtime events.
type StatusEvent struct {
	Kind     string    `json:"kind"`
	ServerID string    `json:"server_id"`
	Status   string    `json:"status"`
	Since    time.Time `json:"since"`
}

// PublishStatus sends a reachability change to server.<id>.status and to
// servers.overview of everyone who has the server in scope.
func (h *Hub) PublishStatus(serverID string, status connmgr.Status, since time.Time) {
	ev := StatusEvent{Kind: "status", ServerID: serverID, Status: string(status), Since: since}
	h.each("status", serverID, func(c *client, s *subscription) {
		h.deliver(c, s, message{Type: "event", Topic: s.topic, Data: ev})
	})
	h.each("overview", "", func(c *client, s *subscription) {
		rights, err := h.perm.For(context.Background(), c.p.UserID)
		if err != nil {
			return
		}
		if _, ok := rights.Server(serverID); ok {
			h.deliver(c, s, message{Type: "event", Topic: s.topic, Data: ev})
		}
	})
}

// PublishRuntime sends the runtime data of a server to servers.overview:
// full to those with agent rights on it, reduced (no agent data) to the
// others in scope (UI-006, revision 2.4 candidate 3).
func (h *Hub) PublishRuntime(serverID string, rcon, admin, reduced any) {
	h.each("overview", "", func(c *client, s *subscription) {
		rights, err := h.perm.For(context.Background(), c.p.UserID)
		if err != nil {
			return
		}
		sr, ok := rights.Server(serverID)
		switch {
		case !ok:
		case sr.Agent && perm.Role(c.p.Role) == perm.RCONAdmin:
			h.deliver(c, s, message{Type: "event", Topic: s.topic, Data: rcon})
		case sr.Agent:
			h.deliver(c, s, message{Type: "event", Topic: s.topic, Data: admin})
		default:
			h.deliver(c, s, message{Type: "event", Topic: s.topic, Data: reduced})
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
// Each subscriber gets it only if his rights let him see a command of its
// author on the server, checked for every message (ROLE-009, D-067).
func (h *Hub) PublishConsole(serverID string, e commands.Entry) {
	ev := ConsoleEvent{
		RequestID: e.RequestID, TS: e.TS, UserID: e.UserID, Pseudonym: e.Pseudonym, Role: e.Role,
		Command: e.Command, Response: e.Response, Truncated: e.Truncated, Result: e.Result, Reason: e.Reason,
	}
	author := perm.AuthorRole(e.Role, "", false)
	h.each("console", serverID, func(c *client, s *subscription) {
		rights, err := h.perm.For(context.Background(), c.p.UserID)
		if err != nil || !rights.CommandVisible(serverID, author) {
			return
		}
		h.deliver(c, s, message{Type: "event", Topic: s.topic, Data: ev})
	})
}
