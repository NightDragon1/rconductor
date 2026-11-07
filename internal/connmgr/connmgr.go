// SPDX-License-Identifier: AGPL-3.0-or-later

// Package connmgr is the connection manager (Architecture 4): exactly one
// persistent connection per game server, with reconnect, a queue that
// sends commands one after the other, and answers that go back to their
// sender (PROT-002, D-010). It also keeps the reachability of each server
// (SRV-004).
package connmgr

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net"
	"sync"
	"time"

	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/rcon"
)

// Status is the reachability of a game server (SRV-004).
type Status string

// The states of a server.
const (
	Connected  Status = "connected"
	AuthFailed Status = "auth_failed"
	Offline    Status = "offline"
)

var (
	// ErrUnknownServer means the server has no connection worker.
	ErrUnknownServer = errors.New("unknown server")
	// ErrUnavailable means there is no RCON connection (Design 3.5
	// server_unavailable).
	ErrUnavailable = errors.New("no rcon connection to the game server")
	// ErrQueueFull means too many commands wait for this server.
	ErrQueueFull = errors.New("too many commands waiting for this server")
)

// Target is what the manager needs to reach a server. Usable is false
// while the credentials are missing or sealed with another master key.
type Target struct {
	ID, Name, Protocol, Addr string
	Password                 logging.Secret
	Usable                   bool
}

// Source provides the targets; the server administration implements it.
type Source interface {
	Targets(ctx context.Context) ([]Target, error)
	Target(ctx context.Context, id string) (Target, bool, error)
}

// Options tune the manager; zero values take the defaults.
type Options struct {
	// ProbeInterval is how often an idle connection is checked with a
	// read that sends nothing.
	ProbeInterval time.Duration
	// IdleProbe is the idle time after which a probe packet is sent; it
	// finds half-open connections a read cannot see (Design 3.8).
	IdleProbe time.Duration
	// ProbeTimeout bounds the wait for the probe's answer.
	ProbeTimeout time.Duration
	// MaxBackoff caps the wait between reconnect attempts; SRV-004 asks
	// for a change to be visible within 10 s.
	MaxBackoff time.Duration
	// QueueSize bounds the waiting commands per server.
	QueueSize int
	// DialTimeout bounds connecting and authenticating.
	DialTimeout time.Duration
	// OnStatus is called on every status change, for the event bus.
	OnStatus func(serverID string, s Status)
}

func (o *Options) defaults() {
	if o.ProbeInterval <= 0 {
		o.ProbeInterval = 2 * time.Second
	}
	if o.IdleProbe <= 0 {
		o.IdleProbe = 30 * time.Second
	}
	if o.ProbeTimeout <= 0 {
		o.ProbeTimeout = 5 * time.Second
	}
	if o.MaxBackoff <= 0 {
		o.MaxBackoff = 5 * time.Second
	}
	if o.QueueSize <= 0 {
		o.QueueSize = 64
	}
	if o.DialTimeout <= 0 {
		o.DialTimeout = 5 * time.Second
	}
}

// Manager holds one worker per server.
type Manager struct {
	adapters rcon.Registry
	source   Source
	log      *slog.Logger
	opts     Options

	mu      sync.Mutex
	ctx     context.Context
	workers map[string]*worker
	wg      sync.WaitGroup
}

// New returns a manager; Start launches the workers.
func New(adapters rcon.Registry, source Source, log *slog.Logger, opts Options) *Manager {
	opts.defaults()
	return &Manager{adapters: adapters, source: source, log: log, opts: opts, workers: map[string]*worker{}}
}

// Start connects to every server and keeps the connections until ctx ends.
func (m *Manager) Start(ctx context.Context) error {
	targets, err := m.source.Targets(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.ctx = ctx
	m.mu.Unlock()
	for _, t := range targets {
		m.ensure(t)
	}
	return nil
}

// Wait blocks until all workers have stopped after the start context ended.
func (m *Manager) Wait() { m.wg.Wait() }

func (m *Manager) ensure(t Target) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if w, ok := m.workers[t.ID]; ok {
		w.update(t)
		return
	}
	w := newWorker(m, t)
	m.workers[t.ID] = w
	m.wg.Go(func() { w.run(m.ctx) })
}

// Reload takes over a changed or new server and reconnects; a deleted
// server loses its worker.
func (m *Manager) Reload(ctx context.Context, id string) error {
	t, ok, err := m.source.Target(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		m.mu.Lock()
		w := m.workers[id]
		delete(m.workers, id)
		m.mu.Unlock()
		if w != nil {
			w.stopWorker()
		}
		return nil
	}
	m.mu.Lock()
	started := m.ctx != nil
	m.mu.Unlock()
	if started {
		m.ensure(t)
	}
	return nil
}

// Status returns the reachability of a server and since when it holds.
func (m *Manager) Status(id string) (Status, time.Time) {
	m.mu.Lock()
	w := m.workers[id]
	m.mu.Unlock()
	if w == nil {
		return Offline, time.Time{}
	}
	return w.state()
}

// tryTimeout bounds a test of RCON data (Design 3.8).
const tryTimeout = 5 * time.Second

// Try opens a connection that only authenticates and closes it at once,
// for a test of RCON data. It is the one exception to one connection per
// server (PROT-002 rev. 2, D-049) and sends no command.
func (m *Manager) Try(ctx context.Context, protocol, addr string, password logging.Secret) error {
	adapter, err := m.adapters.Get(protocol)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, min(m.opts.DialTimeout, tryTimeout))
	defer cancel()
	conn, err := adapter.Dial(ctx, addr, password)
	if err != nil {
		return err
	}
	return conn.Close()
}

// Execute queues a command for a server and waits for its answer, or for
// ctx to end. Commands of all users go through the same connection one
// after the other.
func (m *Manager) Execute(ctx context.Context, id, command string) (string, error) {
	m.mu.Lock()
	w := m.workers[id]
	m.mu.Unlock()
	if w == nil {
		return "", ErrUnknownServer
	}
	if s, _ := w.state(); s != Connected {
		return "", ErrUnavailable
	}
	j := job{ctx: ctx, command: command, reply: make(chan result, 1)}
	select {
	case w.jobs <- j:
	default:
		return "", ErrQueueFull
	}
	select {
	case r := <-j.reply:
		return r.answer, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

type job struct {
	ctx     context.Context
	command string
	reply   chan result
}

type result struct {
	answer string
	err    error
}

type worker struct {
	m      *Manager
	jobs   chan job
	reload chan struct{}
	stop   chan struct{}
	once   sync.Once

	mu     sync.Mutex
	target Target
	status Status
	since  time.Time
}

func newWorker(m *Manager, t Target) *worker {
	return &worker{
		m: m, jobs: make(chan job, m.opts.QueueSize), reload: make(chan struct{}, 1), stop: make(chan struct{}),
		target: t, status: Offline, since: time.Now(),
	}
}

func (w *worker) update(t Target) {
	w.mu.Lock()
	w.target = t
	w.mu.Unlock()
	select {
	case w.reload <- struct{}{}:
	default:
	}
}

func (w *worker) stopWorker() { w.once.Do(func() { close(w.stop) }) }

func (w *worker) state() (Status, time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status, w.since
}

func (w *worker) current() Target {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.target
}

func (w *worker) setStatus(s Status) {
	w.mu.Lock()
	changed := w.status != s
	if changed {
		w.status, w.since = s, time.Now()
	}
	id := w.target.ID
	w.mu.Unlock()
	if changed && w.m.opts.OnStatus != nil {
		w.m.opts.OnStatus(id, s)
	}
}

func (w *worker) attrs(t Target) []any {
	return []any{slog.String(logging.KeyServer, t.ID), slog.String("server_name", t.Name)}
}

// failPending answers all waiting commands with ErrUnavailable.
func (w *worker) failPending() {
	for {
		select {
		case j := <-w.jobs:
			j.reply <- result{err: ErrUnavailable}
		default:
			return
		}
	}
}

// wait sleeps d, answering waiting commands as unavailable; it returns
// false when the worker must end and true early on a reload.
func (w *worker) wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-w.stop:
			return false
		case <-w.reload:
			return true
		case j := <-w.jobs:
			j.reply <- result{err: ErrUnavailable}
		case <-t.C:
			return true
		}
	}
}

// backoff: start 1 s, factor 1.6, jitter ±20 %, at most MaxBackoff
// (Design 1.1, capped for SRV-004).
func (w *worker) backoff(attempt int) time.Duration {
	d := float64(time.Second)
	for range attempt {
		d *= 1.6
	}
	d = min(d, float64(w.m.opts.MaxBackoff))
	d *= 0.8 + 0.4*rand.Float64() //nolint:gosec // jitter, not security
	return time.Duration(d)
}

func (w *worker) run(ctx context.Context) {
	log := w.m.log
	attempt := 0
	lost := false // a connection existed and went away
	for {
		select {
		case <-ctx.Done():
			w.failPending()
			return
		case <-w.stop:
			w.failPending()
			return
		default:
		}
		t := w.current()
		if !t.Usable {
			w.setStatus(Offline)
			if !w.wait(ctx, time.Hour) {
				return
			}
			continue
		}
		if lost && attempt == 0 {
			log.Info("reconnecting to the game server", append(w.attrs(t), logging.Event(logging.RCONReconnect))...)
		}
		adapter, err := w.m.adapters.Get(t.Protocol)
		if err != nil {
			log.Error("no protocol adapter", append(w.attrs(t), slog.Any("err", err))...)
			w.setStatus(Offline)
			if !w.wait(ctx, time.Hour) {
				return
			}
			continue
		}
		dctx, cancel := context.WithTimeout(ctx, w.m.opts.DialTimeout)
		conn, err := adapter.Dial(dctx, t.Addr, t.Password)
		cancel()
		switch {
		case errors.Is(err, rcon.ErrAuth):
			if s, _ := w.state(); s != AuthFailed {
				log.Warn("game server refused the rcon credentials",
					append(w.attrs(t), logging.Event(logging.RCONAuthFailed), slog.String(logging.KeyReason, "auth_failed"))...)
			}
			w.setStatus(AuthFailed)
		case err != nil:
			if s, _ := w.state(); s == Connected {
				lost = true
			}
			w.setStatus(Offline)
			log.Debug("game server not reachable", append(w.attrs(t), slog.Any("err", err))...)
		default:
			attempt = 0
			lost = false
			log.Info("connected to the game server", append(w.attrs(t), logging.Event(logging.RCONConnected))...)
			w.setStatus(Connected)
			reason := w.serve(ctx, conn)
			_ = conn.Close()
			w.setStatus(Offline)
			if reason == "" {
				return // shut down
			}
			if reason != "reload" {
				lost = true
				log.Warn("connection to the game server lost",
					append(w.attrs(t), logging.Event(logging.RCONDisconnected), slog.String(logging.KeyReason, reason))...)
			}
			continue
		}
		if !w.wait(ctx, w.backoff(attempt)) {
			return
		}
		attempt++
	}
}

// serve runs commands on an open connection until it breaks, a reload
// comes or the worker ends (empty reason).
func (w *worker) serve(ctx context.Context, conn rcon.Conn) string {
	probe := time.NewTicker(w.m.opts.ProbeInterval)
	defer probe.Stop()
	used := time.Now() // last traffic with an answer
	for {
		select {
		case <-ctx.Done():
			w.failPending()
			return ""
		case <-w.stop:
			w.failPending()
			return ""
		case <-w.reload:
			return "reload"
		case j := <-w.jobs:
			if err := j.ctx.Err(); err != nil {
				j.reply <- result{err: err}
				continue
			}
			answer, err := conn.Execute(j.ctx, j.command)
			var ne net.Error
			if err != nil && (j.ctx.Err() != nil || (errors.As(err, &ne) && ne.Timeout())) {
				// The socket deadline is the command deadline and may
				// fire a moment before the context does. A late answer
				// is dropped by its packet ID; the connection stays.
				cause := j.ctx.Err()
				if cause == nil {
					cause = context.DeadlineExceeded
				}
				j.reply <- result{err: cause}
				continue
			}
			j.reply <- result{answer: answer, err: err}
			if err != nil && !errors.Is(err, rcon.ErrTooLarge) {
				return "connection_error"
			}
			used = time.Now()
		case <-probe.C:
			if p, ok := conn.(rcon.Prober); ok {
				if err := p.Alive(); err != nil {
					return "closed_by_server"
				}
			}
			if p, ok := conn.(rcon.Pinger); ok && time.Since(used) >= w.m.opts.IdleProbe {
				pctx, cancel := context.WithTimeout(ctx, w.m.opts.ProbeTimeout)
				err := p.Ping(pctx)
				cancel()
				if err != nil {
					return "no_answer"
				}
				used = time.Now()
			}
		}
	}
}
