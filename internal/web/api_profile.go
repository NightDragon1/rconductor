// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/auth/password"
	"github.com/NightDragon1/rconductor/internal/users"
)

func (s *apiServer) GetMe(ctx context.Context, _ api.GetMeRequestObject) (api.GetMeResponseObject, error) {
	u, err := s.profiles.Me(ctx, adminRequest(ctx))
	if err != nil {
		return nil, adminError(err)
	}
	return api.GetMe200JSONResponse(toAPIUser(u)), nil
}

func (s *apiServer) UpdateMe(ctx context.Context, req api.UpdateMeRequestObject) (api.UpdateMeResponseObject, error) {
	if req.Body == nil {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "body required"}
	}
	u, err := s.profiles.Update(ctx, adminRequest(ctx), users.ProfileUpdate{
		Pseudonym: req.Body.Pseudonym, RealName: req.Body.RealName, Contacts: req.Body.Contacts,
	})
	if err != nil {
		return nil, adminError(err)
	}
	return api.UpdateMe200JSONResponse(toAPIUser(u)), nil
}

func (s *apiServer) ChangePassword(ctx context.Context, req api.ChangePasswordRequestObject) (api.ChangePasswordResponseObject, error) {
	if req.Body == nil {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "body required"}
	}
	err := s.profiles.ChangePassword(ctx, adminRequest(ctx), req.Body.CurrentPassword, req.Body.NewPassword)
	switch {
	case errors.Is(err, users.ErrWrongPassword):
		return nil, &problem{status: http.StatusForbidden, reason: api.ReasonCodeForbidden, detail: err.Error()}
	case errors.Is(err, password.ErrBusy):
		return nil, errBusy()
	case err != nil:
		return nil, adminError(err)
	}
	return api.ChangePassword204Response{}, nil
}

func (s *apiServer) SetMyAvatar(ctx context.Context, req api.SetMyAvatarRequestObject) (api.SetMyAvatarResponseObject, error) {
	data, err := io.ReadAll(io.LimitReader(req.Body, users.MaxAvatarBytes+1))
	if err != nil {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "cannot read the image"}
	}
	if err := s.profiles.SetAvatar(ctx, adminRequest(ctx), data); err != nil {
		return nil, adminError(err)
	}
	return api.SetMyAvatar204Response{}, nil
}

func (s *apiServer) DeleteMyAvatar(ctx context.Context, _ api.DeleteMyAvatarRequestObject) (api.DeleteMyAvatarResponseObject, error) {
	if err := s.profiles.SetAvatar(ctx, adminRequest(ctx), nil); err != nil {
		return nil, adminError(err)
	}
	return api.DeleteMyAvatar204Response{}, nil
}

func (s *apiServer) GetUserAvatar(ctx context.Context, req api.GetUserAvatarRequestObject) (api.GetUserAvatarResponseObject, error) {
	data, mediaType, err := s.profiles.Avatar(ctx, adminRequest(ctx), req.Id.String())
	if err != nil {
		return nil, adminError(err)
	}
	// The picture is user content: never sniffed, never active (SEC-006).
	h := httpOf(ctx).w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "private, no-cache")
	h.Set("Content-Disposition", "inline")
	if mediaType == "image/jpeg" {
		return api.GetUserAvatar200ImagejpegResponse{Body: bytes.NewReader(data), ContentLength: int64(len(data))}, nil
	}
	return api.GetUserAvatar200ImagepngResponse{Body: bytes.NewReader(data), ContentLength: int64(len(data))}, nil
}
