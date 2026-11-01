// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NightDragon1/rconductor/internal/api"
)

func TestUnknownAPIPathIsProblem(t *testing.T) {
	h := NewHandler(Options{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q", ct)
	}
	var p api.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Status != http.StatusNotFound || p.Type != "about:blank" || p.Reason == nil || *p.Reason != api.ReasonCodeNotFound {
		t.Errorf("problem = %+v", p)
	}
}

func TestUIMountedAtRoot(t *testing.T) {
	ui := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ui")) })
	h := NewHandler(Options{UI: ui})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/servers/42", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ui" {
		t.Errorf("UI not served: %d %q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/x", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("API path served by UI: %d", rec.Code)
	}
}
