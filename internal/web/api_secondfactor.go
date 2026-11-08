// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/auth"
	"github.com/NightDragon1/rconductor/internal/auth/password"
)

// The cookie of a waiting second step (AUTH-008): like the session cookie
// HttpOnly, SameSite=Lax and over HTTPS with the __Host- prefix.
const (
	pendingName       = "rcd_pending"
	pendingNameSecure = "__Host-rcd_pending"
)

func pendingNameFor(r *http.Request) string {
	if ClientOf(r.Context()).HTTPS {
		return pendingNameSecure
	}
	return pendingName
}

func setPendingCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	//nolint:gosec // Secure only over HTTPS, like the session cookie (W1)
	http.SetCookie(w, &http.Cookie{
		Name: pendingNameFor(r), Value: token, Path: "/", MaxAge: max(int(time.Until(expires).Seconds()), 1),
		HttpOnly: true, Secure: ClientOf(r.Context()).HTTPS, SameSite: http.SameSiteLaxMode,
	})
}

func clearPendingCookie(w http.ResponseWriter, r *http.Request) {
	//nolint:gosec // Secure only over HTTPS, like the session cookie (W1)
	http.SetCookie(w, &http.Cookie{
		Name: pendingNameFor(r), Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: ClientOf(r.Context()).HTTPS, SameSite: http.SameSiteLaxMode,
	})
}

func readPendingCookie(r *http.Request) string {
	c, err := r.Cookie(pendingNameFor(r))
	if err != nil {
		return ""
	}
	return c.Value
}

// whileTOTPSetup are the only operations of a session that must set up the
// second factor first (AUTH-008); no WebSocket either.
var whileTOTPSetup = map[string]bool{
	"GetSession": true, "Logout": true, "GetSecondFactor": true, "SetupSecondFactor": true, "ConfirmSecondFactor": true,
}

func (s *apiServer) authClient(ctx context.Context) auth.Client {
	h := httpOf(ctx)
	return auth.Client{IP: ClientOf(ctx).IP, UserAgent: h.r.UserAgent(), RequestID: RequestID(ctx), SessionToken: readSessionCookie(h.r)}
}

func lockedProblem(locked *auth.LockedError) error {
	return &problem{status: http.StatusTooManyRequests, reason: api.ReasonCodeRateLimited, detail: locked.Error(),
		retryAfter: int(math.Ceil(locked.RetryAfter.Seconds()))}
}

// secondFactorError maps the errors of the own second factor. A wrong
// password or code is 422, not 401: the session is still valid.
func secondFactorError(err error) error {
	var locked *auth.LockedError
	switch {
	case errors.As(err, &locked):
		return lockedProblem(locked)
	case errors.Is(err, auth.ErrInvalidCredentials):
		return &problem{status: http.StatusUnprocessableEntity, reason: api.ReasonCodeInvalid, detail: "password or code wrong"}
	case errors.Is(err, auth.ErrInvalidEmail):
		return &problem{status: http.StatusUnprocessableEntity, reason: api.ReasonCodeInvalid, detail: err.Error()}
	case errors.Is(err, auth.ErrTOTPActive), errors.Is(err, auth.ErrTOTPRequired), errors.Is(err, auth.ErrNoPending):
		return &problem{status: http.StatusConflict, reason: api.ReasonCodeConflict, detail: err.Error()}
	case errors.Is(err, password.ErrBusy):
		return errBusy()
	}
	return err
}

// LoginCode completes a login with the code (AUTH-008).
func (s *apiServer) LoginCode(ctx context.Context, req api.LoginCodeRequestObject) (api.LoginCodeResponseObject, error) {
	if s.auth == nil || req.Body == nil {
		return nil, errUnauthenticated()
	}
	h := httpOf(ctx)
	token, p, err := s.auth.LoginCode(ctx, readPendingCookie(h.r), req.Body.Code, s.authClient(ctx))
	var locked *auth.LockedError
	switch {
	case errors.As(err, &locked):
		clearPendingCookie(h.w, h.r)
		return nil, lockedProblem(locked)
	case errors.Is(err, auth.ErrNoPending):
		clearPendingCookie(h.w, h.r)
		return nil, &problem{status: http.StatusUnauthorized, reason: api.ReasonCodeUnauthenticated, detail: err.Error()}
	case errors.Is(err, auth.ErrInvalidCredentials):
		return nil, &problem{status: http.StatusUnauthorized, reason: api.ReasonCodeUnauthenticated, detail: "code wrong"}
	case errors.Is(err, password.ErrBusy):
		return nil, errBusy()
	case err != nil:
		return nil, err
	}
	clearPendingCookie(h.w, h.r)
	setSessionCookie(h.w, h.r, token, s.auth.MaxLifetime(ctx))
	return api.LoginCode200JSONResponse(s.sessionOf(ctx, p)), nil
}

func (s *apiServer) GetSecondFactor(ctx context.Context, _ api.GetSecondFactorRequestObject) (api.GetSecondFactorResponseObject, error) {
	p, _ := principal(ctx)
	v, err := s.auth.SecondFactorOf(ctx, p)
	if err != nil {
		return nil, err
	}
	return api.GetSecondFactor200JSONResponse(api.SecondFactor{Enabled: v.Enabled, Required: v.Required, Email: v.Email}), nil
}

func (s *apiServer) SetupSecondFactor(ctx context.Context, req api.SetupSecondFactorRequestObject) (api.SetupSecondFactorResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	p, _ := principal(ctx)
	setup, err := s.auth.SetupTOTP(ctx, p, s.authClient(ctx), deref(req.Body.Password))
	if err != nil {
		return nil, secondFactorError(err)
	}
	return api.SetupSecondFactor200JSONResponse{
		Body:    api.SecondFactorSetup{Secret: setup.Secret, Uri: setup.URI},
		Headers: api.SetupSecondFactor200ResponseHeaders{CacheControl: noStore()},
	}, nil
}

func (s *apiServer) ConfirmSecondFactor(ctx context.Context, req api.ConfirmSecondFactorRequestObject) (api.ConfirmSecondFactorResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	p, _ := principal(ctx)
	if err := s.auth.ConfirmTOTP(ctx, p, s.authClient(ctx), req.Body.Code); err != nil {
		return nil, secondFactorError(err)
	}
	return api.ConfirmSecondFactor204Response{}, nil
}

func (s *apiServer) DisableSecondFactor(ctx context.Context, req api.DisableSecondFactorRequestObject) (api.DisableSecondFactorResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	p, _ := principal(ctx)
	code := ""
	if req.Body.Code != nil {
		code = *req.Body.Code
	}
	if err := s.auth.DisableTOTP(ctx, p, s.authClient(ctx), deref(req.Body.Password), code); err != nil {
		return nil, secondFactorError(err)
	}
	return api.DisableSecondFactor204Response{}, nil
}

func (s *apiServer) SetEmail(ctx context.Context, req api.SetEmailRequestObject) (api.SetEmailResponseObject, error) {
	if req.Body == nil {
		return nil, bodyRequired()
	}
	p, _ := principal(ctx)
	code := ""
	if req.Body.Code != nil {
		code = *req.Body.Code
	}
	if err := s.auth.SetEmail(ctx, p, s.authClient(ctx), req.Body.Email, deref(req.Body.Password), code); err != nil {
		return nil, secondFactorError(err)
	}
	return api.SetEmail204Response{}, nil
}

// RequestRecovery answers alike for everyone (AUTH-008).
func (s *apiServer) RequestRecovery(ctx context.Context, req api.RequestRecoveryRequestObject) (api.RequestRecoveryResponseObject, error) {
	if req.Body != nil && s.auth != nil {
		s.auth.RequestRecovery(ctx, req.Body.Pseudonym, s.authClient(ctx))
	}
	return api.RequestRecovery202Response{}, nil
}

func (s *apiServer) RedeemRecovery(ctx context.Context, req api.RedeemRecoveryRequestObject) (api.RedeemRecoveryResponseObject, error) {
	if s.auth == nil || req.Body == nil {
		return nil, bodyRequired()
	}
	err := s.auth.RedeemRecovery(ctx, req.Body.Token, deref(req.Body.Password), s.authClient(ctx))
	var locked *auth.LockedError
	switch {
	case errors.As(err, &locked):
		return nil, lockedProblem(locked)
	case errors.Is(err, auth.ErrRecoveryInvalid), errors.Is(err, auth.ErrInvalidCredentials):
		return nil, &problem{status: http.StatusUnauthorized, reason: api.ReasonCodeUnauthenticated, detail: "link or password not valid"}
	case errors.Is(err, password.ErrBusy):
		return nil, errBusy()
	case err != nil:
		return nil, err
	}
	return api.RedeemRecovery204Response{}, nil
}

func (s *apiServer) ResetSecondFactor(ctx context.Context, req api.ResetSecondFactorRequestObject) (api.ResetSecondFactorResponseObject, error) {
	if err := s.profiles.ResetSecondFactor(ctx, adminRequest(ctx), req.Id.String()); err != nil {
		return nil, adminError(err)
	}
	return api.ResetSecondFactor204Response{}, nil
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func noStore() *string {
	v := "no-store"
	return &v
}
