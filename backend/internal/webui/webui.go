// Package webui serves the built frontend, which is embedded into the binary.
//
// The files in dist/ are produced by `npm run build` in web/. A fresh checkout only holds
// dist/.gitkeep, so the package compiles without a frontend build; the handler then answers 503.
package webui

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

func init() {
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// Handler serves the embedded frontend.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // dist is part of the embed pattern, so this cannot happen
	}
	return NewHandler(sub)
}

// NewHandler serves a frontend build from fsys. Unknown paths fall back to index.html
// so the app can handle them, except under /api/, which stays a plain 404.
func NewHandler(fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if _, err := fs.Stat(fsys, "index.html"); err != nil {
			http.Error(w, "frontend not built: run `npm run build` in web/", http.StatusServiceUnavailable)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if st, err := fs.Stat(fsys, name); err != nil || st.IsDir() {
			if strings.HasPrefix(name, "api/") || path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
			name = "index.html"
		}
		// Hashed build files never change; everything else (index.html, the service worker, the
		// manifest) must be revalidated so an update reaches the browsers.
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeFileFS(w, r, fsys, name)
	})
}
