// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NightDragon1/rconductor/internal/agentproto"
	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/auth"
	"github.com/NightDragon1/rconductor/internal/diag"
	"github.com/NightDragon1/rconductor/internal/store/storetest"
)

// heldAgent answers the log of the agent only when released.
type heldAgent struct{ asked, release chan struct{} }

func (h heldAgent) RequestData(ctx context.Context, _, _ string, _ any, _ func(agentproto.Envelope)) (agentproto.Result, error) {
	close(h.asked)
	select {
	case <-h.release:
	case <-ctx.Done():
	}
	return agentproto.Result{}, errors.New("released")
}

// TestC338ExportBusyOverHTTP: while diagnostics.parallel packages are
// built, another export is 503 with Retry-After (D-090).
func TestC338ExportBusyOverHTTP(t *testing.T) {
	st := storetest.New(t)
	admin := storetest.CreateUser(t, st, storetest.User{Pseudonym: "a", Role: "rcon_admin"})
	if _, err := st.DB.Exec(`INSERT INTO settings (id, key, value, updated_at) VALUES ('p', 'diagnostics.parallel', '1', 't')`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`INSERT INTO agents (id, name, public_key, fingerprint, status, remote, created_at) VALUES ('00000000-0000-4000-8000-0000000000a1', 'a', x'00', 'fp', 'active', '', 't')`); err != nil {
		t.Fatal(err)
	}
	h := heldAgent{asked: make(chan struct{}), release: make(chan struct{})}
	d := diag.NewService(st, nil)
	d.Use(h, nil)
	s := &apiServer{diag: d}
	ctx := context.WithValue(context.Background(), principalKey{}, auth.Principal{UserID: admin, Role: "rcon_admin", Pseudonym: "a", SessionHash: "0123456789abcdef"})
	first := make(chan error, 1)
	go func() {
		_, err := s.GetDiagnostics(ctx, api.GetDiagnosticsRequestObject{})
		first <- err
	}()
	<-h.asked
	_, err := s.GetDiagnostics(ctx, api.GetDiagnosticsRequestObject{})
	close(h.release)
	if err := <-first; err != nil {
		t.Errorf("first export = %v", err)
	}
	rec := httptest.NewRecorder()
	s.writeError(rec, httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics", nil), err)
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Errorf("second export = %d, Retry-After %q, want 503 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}
}
