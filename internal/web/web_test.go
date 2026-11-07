// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

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

	for _, target := range []string{"/api/v1/x", "/api", "/api/v2/x", "/healthz", "/healthz/live", "/metrics", "/metrics/x"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusNotFound || rec.Body.String() == "ui" {
			t.Errorf("%s served by UI: %d", target, rec.Code)
		}
	}
}

func TestRequestID(t *testing.T) {
	var seen string
	ui := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = RequestID(r.Context()) })
	h := NewHandler(Options{UI: ui})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderRequestID, "forged")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	id := rec.Header().Get(HeaderRequestID)
	if id == "" || id == "forged" || id != seen {
		t.Errorf("request id header %q, context %q", id, seen)
	}
	if u, err := uuid.Parse(id); err != nil || u.Version() != 7 {
		t.Errorf("request id %q is not a UUIDv7", id)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/x", nil))
	var p api.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.RequestId == nil || *p.RequestId != rec.Header().Get(HeaderRequestID) {
		t.Errorf("problem request_id = %v, header %q", p.RequestId, rec.Header().Get(HeaderRequestID))
	}
}
