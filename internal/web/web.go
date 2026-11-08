// SPDX-License-Identifier: AGPL-3.0-or-later

// Package web is the web layer of the panel: HTTP server, API routing and
// delivery of the embedded UI. The web port speaks plain HTTP only; TLS is
// the job of a reverse proxy (D-011).
package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/NightDragon1/rconductor/internal/agents"
	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/auth"
	"github.com/NightDragon1/rconductor/internal/cache"
	"github.com/NightDragon1/rconductor/internal/commands"
	"github.com/NightDragon1/rconductor/internal/events"
	"github.com/NightDragon1/rconductor/internal/mail"
	"github.com/NightDragon1/rconductor/internal/netlimit"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/servers"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/users"
)

// APIBase is the base path of the REST API (Design 2.1).
const APIBase = "/api/v1"

const shutdownTimeout = 10 * time.Second

// writeTimeout makes a client that does not read its answers give its
// connection back (T-025); WebSockets clear this deadline.
var writeTimeout = 60 * time.Second

// reservedPaths are never served by the UI: the API (any version) and the
// health and metrics endpoints planned with NFR-006.
var reservedPaths = []string{"/api", "/api/", "/healthz", "/healthz/", "/metrics", "/metrics/"}

// Options configure the HTTP handler.
type Options struct {
	Log *slog.Logger
	// UI serves the web interface; nil means no UI is mounted.
	UI http.Handler
	// Proxies decide which forwarding headers are trusted.
	Proxies Proxies
	// Hub serves the live WebSockets.
	Hub *events.Hub
	// Cache holds runtime data such as player lists.
	Cache *cache.Cache
	// Commands runs the command pipeline.
	Commands *commands.Service
	// Servers administers game servers and server groups.
	Servers *servers.Service
	// Store gives read access for listings that need no service.
	Store *store.Store
	// Auth resolves sessions; nil means every API call is unauthenticated.
	Auth *auth.Service
	// Perm decides about every access to servers and administration.
	Perm *perm.Checker
	// Users administers users and assignments.
	Users *users.Service
	// Mail is the SMTP setup and the test mail (NFR-017).
	Mail *mail.Service
	// Invites creates and redeems invitations.
	Invites *users.Invites
	// Profiles maintains the caller's own profile and password.
	Profiles *users.Profiles
	// Agents administers the host agents.
	Agents *agents.Service
	// Runtime provides the runtime data of servers.
	Runtime RuntimeSource
}

// NewHandler returns the root handler of the web port.
func NewHandler(opts Options) http.Handler {
	mux := http.NewServeMux()

	// Unknown API paths and paths reserved for operation endpoints answer
	// with Problem Details and never fall through to the UI.
	notFound := func(w http.ResponseWriter, _ *http.Request) {
		WriteProblem(w, http.StatusNotFound, api.ReasonCodeNotFound, "")
	}
	for _, p := range reservedPaths {
		mux.HandleFunc(p, notFound)
	}
	srv := &apiServer{agents: opts.Agents, runtime: opts.Runtime, hub: opts.Hub, cache: opts.Cache, commands: opts.Commands, servers: opts.Servers, store: opts.Store, auth: opts.Auth, perm: opts.Perm, users: opts.Users, mail: opts.Mail, invites: opts.Invites, profiles: opts.Profiles, log: opts.Log, proxies: opts.Proxies, decode: semaphore.NewWeighted(decodeBudget), pictures: semaphore.NewWeighted(pictureBudget)}
	api.HandlerWithOptions(srv.handler(), api.StdHTTPServerOptions{
		BaseURL:     APIBase,
		BaseRouter:  mux,
		Middlewares: []api.MiddlewareFunc{srv.limitBodies, srv.requireSession, srv.checkOrigin},
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			WriteProblem(w, http.StatusBadRequest, api.ReasonCodeInvalid, err.Error())
		},
	})

	if opts.UI != nil {
		mux.Handle("/", opts.UI)
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			WriteProblem(w, http.StatusNotFound, "", "")
		})
	}
	return withSecurityHeaders(withRequestID(withBodyLimit(withClient(opts.Proxies, mux))))
}

type connKey struct{}

// keepConn exempts the connection of a request from the age limit of
// connections without a session.
func keepConn(ctx context.Context) {
	if c, ok := ctx.Value(connKey{}).(net.Conn); ok {
		netlimit.Keep(c)
	}
}

// Serve listens on addr and serves h within the connection limits until ctx
// ends, then shuts down gracefully. ready, if set, receives the bound address.
func Serve(ctx context.Context, addr string, limits netlimit.Options, h http.Handler, log *slog.Logger, ready func(net.Addr)) error {
	var lc net.ListenConfig
	tcp, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	limits.Log = log
	ln := netlimit.New(tcp, limits)
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return context.WithValue(ctx, connKey{}, c)
		},
		ConnState: func(c net.Conn, state http.ConnState) {
			switch state {
			case http.StateActive:
				netlimit.Busy(c, true)
			case http.StateIdle:
				netlimit.Busy(c, false)
			case http.StateHijacked:
				// Only a session gets a WebSocket, and it runs on.
				netlimit.Keep(c)
			}
		},
	}
	if ready != nil {
		ready(ln.Addr())
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case err := <-errc:
		return fmt.Errorf("serve http: %w", err)
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return fmt.Errorf("shut down http: %w", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve http: %w", err)
	}
	return nil
}
