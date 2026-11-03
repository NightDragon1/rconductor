// SPDX-License-Identifier: AGPL-3.0-or-later

// Package webui serves the web interface that is built from web/ and
// embedded into the panel binary (D-002).
package webui

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"html"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/NightDragon1/rconductor/internal/web"
)

// dist holds the vite build; in a fresh checkout it contains only the
// .gitkeep placeholder, which is why all: is needed.
//
//go:embed all:dist
var dist embed.FS

const indexFile = "index.html"

// Policy is the Content-Security-Policy of the interface (SEC-009): only
// own scripts, styles, fonts and images, no inline script, the WebSocket
// to the panel itself, never framed. The page itself gets PagePolicy.
const Policy = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; font-src 'self'; " +
	"connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

// PagePolicy is Policy for index.html with a nonce for style elements
// only: dialogs of the component library add a style element to lock
// scrolling. Scripts stay 'self' without exception.
func PagePolicy(nonce string) string {
	return strings.Replace(Policy, "style-src 'self'", "style-src 'self' 'nonce-"+nonce+"'", 1)
}

// NonceMeta names the meta element that hands the style nonce to the
// interface.
const NonceMeta = "rcd-style-nonce"

// Handler serves the embedded build below basePath (RCD_BASE_PATH).
func Handler(basePath string) http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // the directory is embedded at compile time
	}
	return NewAt(sub, basePath)
}

// New serves the UI from fsys. Paths with a file extension are assets and
// answer 404 when missing; all other paths get index.html so client-side
// routes work after a reload. Without index.html the UI is not built and
// every page answers 503.
func New(fsys fs.FS) http.Handler { return NewAt(fsys, "/") }

// NewAt is New with the base path the browser sees: index.html gets
// <base href="{basePath}/">, so that relative assets and client routes
// resolve from deep links too (issue #88), without an inline script.
func NewAt(fsys fs.FS, basePath string) http.Handler {
	index, ok := withBase(fsys, basePath)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", Policy)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			web.WriteProblem(w, http.StatusMethodNotAllowed, "", "")
			return
		}

		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" || name == indexFile || path.Ext(name) == "" {
			serveIndex(w, r, index, ok)
			return
		}
		if hidden(name) || !serveFile(w, r, fsys, name, "public, max-age=31536000, immutable") {
			web.WriteProblem(w, http.StatusNotFound, "", "")
		}
	})
}

// nonceSlot marks where each answer puts its nonce meta element.
const nonceSlot = "<!--rcd-nonce-->"

// withBase reads index.html once and puts the base element first into its
// head.
func withBase(fsys fs.FS, basePath string) ([]byte, bool) {
	b, err := fs.ReadFile(fsys, indexFile)
	if err != nil {
		return nil, false
	}
	href := strings.TrimSuffix(basePath, "/") + "/"
	tag := `<base href="` + html.EscapeString(href) + `">` + nonceSlot
	s := string(b)
	if i := strings.Index(s, "<head>"); i >= 0 {
		s = s[:i+len("<head>")] + tag + s[i+len("<head>"):]
	} else {
		s = tag + s
	}
	return []byte(s), true
}

func serveIndex(w http.ResponseWriter, r *http.Request, index []byte, ok bool) {
	if !ok {
		web.WriteProblem(w, http.StatusServiceUnavailable, "", "web UI is not built")
		return
	}
	// A fresh nonce for every page (SEC-009).
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		web.WriteProblem(w, http.StatusInternalServerError, "", "")
		return
	}
	nonce := base64.RawStdEncoding.EncodeToString(b)
	w.Header().Set("Content-Security-Policy", PagePolicy(nonce))
	index = bytes.Replace(index, []byte(nonceSlot), []byte(`<meta name="`+NonceMeta+`" content="`+nonce+`">`), 1)
	// The index references hashed assets and must always be revalidated.
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, indexFile, time.Time{}, bytes.NewReader(index))
}

// serveFile writes the regular file name and reports whether it exists.
func serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name, cacheControl string) bool {
	f, err := fsys.Open(name)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		return false
	}
	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, time.Time{}, rs)
	return true
}

// hidden reports whether any path segment starts with a dot, such as the
// .gitkeep placeholder.
func hidden(name string) bool {
	for seg := range strings.SplitSeq(name, "/") {
		if strings.HasPrefix(seg, ".") {
			return true
		}
	}
	return false
}
