package api

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// dist is populated at build time from web/dist via the Dockerfile COPY step.
// The embed directive expects the dist/ folder to exist relative to this file.
// During Docker build: COPY --from=frontend /web/dist ./internal/api/dist
//
//go:embed all:dist
var staticFiles embed.FS

// staticHandler serves the embedded React app.
// Any path not starting with /api/ that has no matching file falls back to
// index.html so the React router handles it client-side.
func staticHandler() http.Handler {
	dist, err := fs.Sub(staticFiles, "dist")
	if err != nil {
		panic("static: could not sub dist: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(dist))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := dist.Open(path); err != nil {
			// File not found — serve index.html for client-side routing.
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
