// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/auth"
	"github.com/NightDragon1/rconductor/internal/auth/password"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Session cookie names (decision W1): the __Host- prefix binds the cookie
// to this host and path / and requires Secure, so it is only used over
// HTTPS.
const (
	cookieName       = "rcd_session"
	cookieNameSecure = "__Host-rcd_session"
)

func cookieNameFor(r *http.Request) string {
	if ClientOf(r.Context()).HTTPS {
		return cookieNameSecure
	}
	return cookieName
}

// readSessionCookie reads only the cookie name that fits the connection,
// so over HTTPS a plain cookie set by another host cannot fixate a session.
func readSessionCookie(r *http.Request) string {
	c, err := r.Cookie(cookieNameFor(r))
	if err != nil {
		return ""
	}
	return c.Value
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, maxLifetime time.Duration) {
	//nolint:gosec // Secure only over HTTPS, otherwise login over plain HTTP would be impossible (W1)
	http.SetCookie(w, &http.Cookie{
		Name: cookieNameFor(r), Value: token, Path: "/", MaxAge: int(maxLifetime.Seconds()),
		HttpOnly: true, Secure: ClientOf(r.Context()).HTTPS, SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	//nolint:gosec // Secure only over HTTPS, otherwise login over plain HTTP would be impossible (W1)
	http.SetCookie(w, &http.Cookie{
		Name: cookieNameFor(r), Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: ClientOf(r.Context()).HTTPS, SameSite: http.SameSiteLaxMode,
	})
}

func (s *apiServer) sessionOf(ctx context.Context, p auth.Principal) api.Session {
	id, _ := uuid.Parse(p.UserID)
	out := api.Session{
		User:               api.SessionUser{Id: id, Pseudonym: p.Pseudonym, Role: api.Role(p.Role)},
		ExpiresAt:          p.ExpiresAt,
		MustChangePassword: p.MustChangePassword,
	}
	if s.store != nil {
		var on, supporters bool
		if s.store.GlobalSetting(ctx, users.SettingShortcuts, &on) == nil {
			out.Ui.ShortcutsEnabled = on
		}
		if s.store.GlobalSetting(ctx, perm.SettingSupportersEnabled, &supporters) == nil {
			out.Ui.SupportersEnabled = supporters
		}
	}
	return out
}

func (s *apiServer) Login(ctx context.Context, req api.LoginRequestObject) (api.LoginResponseObject, error) {
	if s.auth == nil {
		return nil, errUnauthenticated()
	}
	if req.Body == nil || req.Body.Pseudonym == "" || req.Body.Password == "" {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "pseudonym and password are required"}
	}
	h := httpOf(ctx)
	client := ClientOf(ctx)
	token, p, err := s.auth.Login(ctx, req.Body.Pseudonym, req.Body.Password, auth.Client{
		IP: client.IP, UserAgent: h.r.UserAgent(), RequestID: RequestID(ctx),
	})
	var locked *auth.LockedError
	switch {
	case errors.As(err, &locked):
		return nil, &problem{
			status: http.StatusTooManyRequests, reason: api.ReasonCodeRateLimited, detail: locked.Error(),
			retryAfter: int(math.Ceil(locked.RetryAfter.Seconds())),
		}
	case errors.Is(err, auth.ErrInvalidCredentials):
		return nil, &problem{status: http.StatusUnauthorized, reason: api.ReasonCodeUnauthenticated, detail: auth.ErrInvalidCredentials.Error()}
	case errors.Is(err, password.ErrBusy):
		return nil, errBusy()
	case err != nil:
		return nil, err
	}
	setSessionCookie(h.w, h.r, token, s.auth.MaxLifetime(ctx))
	return api.Login200JSONResponse(s.sessionOf(ctx, p)), nil
}

func (s *apiServer) Logout(ctx context.Context, _ api.LogoutRequestObject) (api.LogoutResponseObject, error) {
	p, _ := principal(ctx)
	if err := s.auth.Logout(ctx, p, RequestID(ctx)); err != nil {
		return nil, err
	}
	h := httpOf(ctx)
	clearSessionCookie(h.w, h.r)
	return api.Logout204Response{}, nil
}

func (s *apiServer) GetSession(ctx context.Context, _ api.GetSessionRequestObject) (api.GetSessionResponseObject, error) {
	p, _ := principal(ctx)
	return api.GetSession200JSONResponse(s.sessionOf(ctx, p)), nil
}
