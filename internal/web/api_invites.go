// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/auth/password"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/users"
)

func toAPIInvite(i users.Invite) api.Invite {
	id, _ := uuid.Parse(i.ID)
	by, _ := uuid.Parse(i.CreatedBy)
	return api.Invite{
		Id: id, CreatedBy: by, Role: api.Role(i.Role), TemplateUserId: uuidPtr(i.TemplateUserID),
		CreatedAt: i.CreatedAt, ExpiresAt: i.ExpiresAt, UsedAt: i.UsedAt, UsedBy: uuidPtr(i.UsedBy),
	}
}

// inviteURL is the link for an invitee; the token goes into the fragment,
// which browsers never send to a server.
func (s *apiServer) inviteURL(ctx context.Context, token string) string {
	base := s.expectedOrigin(httpOf(ctx).r)
	if u := s.proxies.PublicURL; u != nil {
		base = u.String()
	}
	for len(base) > 0 && base[len(base)-1] == '/' {
		base = base[:len(base)-1]
	}
	return base + "/invite#" + token
}

func (s *apiServer) ListInvites(ctx context.Context, _ api.ListInvitesRequestObject) (api.ListInvitesResponseObject, error) {
	list, err := s.invites.List(ctx, adminRequest(ctx))
	if err != nil {
		return nil, adminError(err)
	}
	out := api.InviteList{Items: make([]api.Invite, 0, len(list))}
	for _, i := range list {
		out.Items = append(out.Items, toAPIInvite(i))
	}
	return api.ListInvites200JSONResponse(out), nil
}

func (s *apiServer) CreateInvite(ctx context.Context, req api.CreateInviteRequestObject) (api.CreateInviteResponseObject, error) {
	if req.Body == nil {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "body required"}
	}
	inv, token, err := s.invites.Create(ctx, adminRequest(ctx), perm.Role(req.Body.Role), idOf(req.Body.TemplateUserId))
	if err != nil {
		return nil, adminError(err)
	}
	return api.CreateInvite201JSONResponse(api.InviteCreated{Invite: toAPIInvite(inv), Token: token, Url: s.inviteURL(ctx, token)}), nil
}

func (s *apiServer) DeleteInvite(ctx context.Context, req api.DeleteInviteRequestObject) (api.DeleteInviteResponseObject, error) {
	if err := s.invites.Delete(ctx, adminRequest(ctx), req.Id.String()); err != nil {
		return nil, adminError(err)
	}
	return api.DeleteInvite204Response{}, nil
}

func (s *apiServer) RedeemInvite(ctx context.Context, req api.RedeemInviteRequestObject) (api.RedeemInviteResponseObject, error) {
	if req.Body == nil || s.invites == nil {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "body required"}
	}
	u, err := s.invites.Redeem(ctx, req.Body.Token, req.Body.Pseudonym, req.Body.Password, RequestID(ctx))
	switch {
	case errors.Is(err, users.ErrInviteInvalid):
		return nil, &problem{status: http.StatusNotFound, reason: api.ReasonCodeNotFound, detail: err.Error()}
	case errors.Is(err, password.ErrBusy):
		return nil, errBusy()
	case err != nil:
		return nil, adminError(err)
	}
	id, _ := uuid.Parse(u.ID)
	return api.RedeemInvite201JSONResponse(api.SessionUser{Id: id, Pseudonym: u.Pseudonym, Role: api.Role(u.Role)}), nil
}
