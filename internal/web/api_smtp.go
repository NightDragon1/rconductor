// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/mail"
)

func toAPISMTP(v mail.View) api.SMTPSettings {
	return api.SMTPSettings{Host: v.Host, Port: v.Port, Encryption: api.SMTPSettingsEncryption(v.Encryption),
		Username: v.Username, PasswordSet: v.PasswordSet, From: v.From, Version: versionString(v.Version)}
}

func mailError(err error) error {
	if errors.Is(err, mail.ErrRateLimited) {
		return &problem{status: http.StatusTooManyRequests, reason: api.ReasonCodeRateLimited, retryAfter: 60}
	}
	return adminError(err)
}

// GetSMTPSettings shows the SMTP setup without the password (NFR-017).
func (s *apiServer) GetSMTPSettings(ctx context.Context, _ api.GetSMTPSettingsRequestObject) (api.GetSMTPSettingsResponseObject, error) {
	if s.mail == nil {
		return nil, errors.New("mail not configured")
	}
	v, err := s.mail.Get(ctx, adminRequest(ctx))
	if err != nil {
		return nil, mailError(err)
	}
	return api.GetSMTPSettings200JSONResponse(toAPISMTP(v)), nil
}

// UpdateSMTPSettings changes the SMTP setup with If-Match (NFR-017).
func (s *apiServer) UpdateSMTPSettings(ctx context.Context, req api.UpdateSMTPSettingsRequestObject) (api.UpdateSMTPSettingsResponseObject, error) {
	if s.mail == nil {
		return nil, errors.New("mail not configured")
	}
	if req.Body == nil {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "body required"}
	}
	b := req.Body
	in := mail.Input{Host: b.Host, Port: b.Port, Username: b.Username, From: b.From}
	if b.Encryption != nil {
		e := string(*b.Encryption)
		in.Encryption = &e
	}
	if b.Password != nil {
		pw := logging.Secret(*b.Password)
		in.Password = &pw
	}
	v, err := s.mail.Update(ctx, adminRequest(ctx), in, ifMatch(req.Params.IfMatch))
	if err != nil {
		return nil, mailError(err)
	}
	return api.UpdateSMTPSettings200JSONResponse(toAPISMTP(v)), nil
}

// TestSMTP sends the fixed test mail; a failure names only its category
// (NFR-017).
func (s *apiServer) TestSMTP(ctx context.Context, req api.TestSMTPRequestObject) (api.TestSMTPResponseObject, error) {
	if s.mail == nil {
		return nil, errors.New("mail not configured")
	}
	if req.Body == nil {
		return nil, &problem{status: http.StatusBadRequest, reason: api.ReasonCodeInvalid, detail: "body required"}
	}
	r, err := s.mail.Test(ctx, adminRequest(ctx), req.Body.To)
	if err != nil {
		return nil, mailError(err)
	}
	out := api.TestSMTP200JSONResponse{Ok: r.OK}
	if r.Category != "" {
		c := api.SMTPTestResultCategory(r.Category)
		out.Category = &c
	}
	return out, nil
}
