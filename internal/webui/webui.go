// SPDX-License-Identifier: AGPL-3.0-or-later

// Package webui serves the web interface that is built from web/ and
// embedded into the panel binary (D-002).
package webui

import (
	"embed"
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

// Handler serves the embedded build.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // the directory is embedded at compile time
	}
	return New(sub)
}

// New serves the UI from fsys. Paths with a file extension are assets and
// answer 404 when missing; all other paths get index.html so client-side
// routes work after a reload. Without index.html the UI is not built and
// every page answers 503.
func New(fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			web.WriteProblem(w, http.StatusMethodNotAllowed, "", "")
			return
		}

		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" || name == indexFile || path.Ext(name) == "" {
			serveIndex(w, r, fsys)
			return
		}
		if hidden(name) || !serveFile(w, r, fsys, name, "public, max-age=31536000, immutable") {
			web.WriteProblem(w, http.StatusNotFound, "", "")
		}
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, fsys fs.FS) {
	// The index references hashed assets and must always be revalidated.
	if !serveFile(w, r, fsys, indexFile, "no-cache") {
		web.WriteProblem(w, http.StatusServiceUnavailable, "", "web UI is not built")
	}
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
