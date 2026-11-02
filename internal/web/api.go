// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/auth"
	"github.com/NightDragon1/rconductor/internal/commands"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/servers"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/users"
)

// publicOperations need no session; every other operation is rejected
// with 401 unless a valid session is present (default deny).
// The names are the generated Go method names of the operations.
var publicOperations = map[string]bool{"Login": true, "RedeemInvite": true}

// whileMustChange are the only operations a user with a forced password
// change may call (AUTH-010).
var whileMustChange = map[string]bool{"ChangePassword": true, "Logout": true, "GetSession": true}

// apiServer implements the generated strict server interface.
type apiServer struct {
	commands *commands.Service
	servers  *servers.Service
	store    *store.Store
	auth     *auth.Service
	perm     *perm.Checker
	users    *users.Service
	invites  *users.Invites
	profiles *users.Profiles
	log      *slog.Logger
	proxies  Proxies
}

var _ api.StrictServerInterface = (*apiServer)(nil)

func (s *apiServer) handler() api.ServerInterface {
	return api.NewStrictHandlerWithOptions(s, []api.StrictMiddlewareFunc{s.authenticate}, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			WriteProblem(w, http.StatusBadRequest, api.ReasonCodeInvalid, err.Error())
		},
		ResponseErrorHandlerFunc: s.writeError,
	})
}

// problem is an error that maps to Problem Details.
type problem struct {
	status     int
	reason     api.ReasonCode
	detail     string
	retryAfter int // seconds; 0 means no Retry-After header
}

func (p *problem) Error() string { return p.detail }

// errBusy answers when the panel is out of capacity, for example all
// password hashing slots are in use (Design 3.5).
func errBusy() error {
	return &problem{status: http.StatusServiceUnavailable, reason: api.ReasonCodeBusy, detail: "busy, try again", retryAfter: 1}
}

func errUnauthenticated() error {
	return &problem{status: http.StatusUnauthorized, reason: api.ReasonCodeUnauthenticated}
}

func (s *apiServer) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var p *problem
	if errors.As(err, &p) {
		if p.retryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(p.retryAfter))
		}
		WriteProblem(w, p.status, p.reason, p.detail)
		return
	}
	if s.log != nil {
		s.log.Error("request failed", slog.String(logging.KeyRequestID, RequestID(r.Context())), slog.Any("err", err))
	}
	WriteProblem(w, http.StatusInternalServerError, "", "")
}

type httpKey struct{}

type httpPair struct {
	w http.ResponseWriter
	r *http.Request
}

type principalKey struct{}

// principal returns the authenticated user of a request.
func principal(ctx context.Context) (auth.Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(auth.Principal)
	return p, ok
}

// authenticate resolves the session cookie for every operation, rejects
// non-public operations without a session, and makes the HTTP request and
// response writer available to the handlers (for cookies).
func (s *apiServer) authenticate(next api.StrictHandlerFunc, operationID string) api.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		ctx = context.WithValue(ctx, httpKey{}, httpPair{w: w, r: r})
		if s.auth != nil {
			if token := readSessionCookie(r); token != "" {
				p, err := s.auth.Authenticate(ctx, token)
				switch {
				case err == nil:
					ctx = context.WithValue(ctx, principalKey{}, p)
					// AUTH-006: every request is tied to its session in the log.
					s.log.Debug("request", slog.String(logging.KeyRequestID, RequestID(ctx)),
						slog.String(logging.KeySession, p.SessionRef()), slog.String(logging.KeyUser, p.Pseudonym),
						slog.String(logging.KeyRole, p.Role), slog.String("operation", operationID))
				case errors.Is(err, auth.ErrUnauthenticated):
					clearSessionCookie(w, r)
				default:
					return nil, err
				}
			}
		}
		p, ok := principal(ctx)
		if !ok && !publicOperations[operationID] {
			return nil, errUnauthenticated()
		}
		if ok && p.MustChangePassword && !whileMustChange[operationID] {
			perm.LogDenied(s.log, perm.Denial{
				RequestID: RequestID(ctx), SessionRef: p.SessionRef(), User: p.Pseudonym, Role: perm.Role(p.Role),
				Reason: string(api.ReasonCodePasswordChangeRequired), Action: operationID,
			})
			return nil, &problem{status: http.StatusForbidden, reason: api.ReasonCodePasswordChangeRequired,
				detail: "change your password first"}
		}
		return next(ctx, w, r, request)
	}
}

func httpOf(ctx context.Context) httpPair {
	h, _ := ctx.Value(httpKey{}).(httpPair)
	return h
}

// checkOrigin rejects write requests whose Origin header does not match the
// panel (CSRF protection together with SameSite, Design 2.1). Browsers send
// Origin on every cross-site and same-site write request.
func (s *apiServer) checkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin == "" || origin != s.expectedOrigin(r) {
			WriteProblem(w, http.StatusForbidden, api.ReasonCodeForbidden, "origin not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// expectedOrigin is the origin of RCD_PUBLIC_URL, or else the scheme and
// host the request reached the panel with.
func (s *apiServer) expectedOrigin(r *http.Request) string {
	if u := s.proxies.PublicURL; u != nil {
		return u.Scheme + "://" + u.Host
	}
	scheme := "http"
	if ClientOf(r.Context()).HTTPS {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
