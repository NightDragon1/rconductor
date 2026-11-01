// SPDX-License-Identifier: AGPL-3.0-or-later

package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

var built = fstest.MapFS{
	"index.html":       {Data: []byte("<!doctype html><title>ui</title>")},
	"assets/app-1.js":  {Data: []byte("console.log(1)")},
	"assets/app-1.css": {Data: []byte("body{}")},
	".gitkeep":         {Data: nil},
}

func get(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestIndexAndClientRoutes(t *testing.T) {
	h := New(built)
	for _, target := range []string{"/", "/index.html", "/servers/42", "/admin/users/", "/a/../b"} {
		rec := get(t, h, http.MethodGet, target)
		if rec.Code != http.StatusOK || rec.Body.String() != string(built["index.html"].Data) {
			t.Errorf("%s: %d %q, want index.html", target, rec.Code, rec.Body.String())
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("%s: Cache-Control = %q", target, cc)
		}
	}
}

func TestAssets(t *testing.T) {
	h := New(built)
	rec := get(t, h, http.MethodGet, "/assets/app-1.js")
	if rec.Code != http.StatusOK || rec.Body.String() != "console.log(1)" {
		t.Fatalf("asset: %d %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q", cc)
	}
}

func TestMissingAssetIsNotIndex(t *testing.T) {
	h := New(built)
	for _, target := range []string{"/assets/gone.js", "/style.css", "/favicon.ico", "/servers/42/x.png", "/.gitkeep", "/assets"} {
		rec := get(t, h, http.MethodGet, target)
		if target == "/assets" {
			// A directory without extension is a client route.
			if rec.Code != http.StatusOK {
				t.Errorf("%s: %d", target, rec.Code)
			}
			continue
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", target, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("%s: Content-Type = %q", target, ct)
		}
	}
}

func TestNotBuilt(t *testing.T) {
	h := New(fstest.MapFS{".gitkeep": {}})
	for _, target := range []string{"/", "/servers/42"} {
		if rec := get(t, h, http.MethodGet, target); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: %d, want 503", target, rec.Code)
		}
	}
	if rec := get(t, h, http.MethodGet, "/assets/app.js"); rec.Code != http.StatusNotFound {
		t.Errorf("asset without build: %d, want 404", rec.Code)
	}
}

func TestOnlyGetAndHead(t *testing.T) {
	h := New(built)
	if rec := get(t, h, http.MethodHead, "/"); rec.Code != http.StatusOK {
		t.Errorf("HEAD: %d", rec.Code)
	}
	rec := get(t, h, http.MethodPost, "/")
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("POST: %d Allow=%q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestEmbeddedHandler(t *testing.T) {
	// Works with and without a UI build in internal/webui/dist.
	rec := get(t, Handler(), http.MethodGet, "/")
	if rec.Code != http.StatusOK && rec.Code != http.StatusServiceUnavailable {
		t.Errorf("embedded: %d", rec.Code)
	}
	if rec := get(t, Handler(), http.MethodGet, "/.gitkeep"); rec.Code != http.StatusNotFound {
		t.Errorf("placeholder served: %d", rec.Code)
	}
}
