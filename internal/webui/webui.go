// Package webui serves the embedded single-page application.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"gitsync/internal/respond"
)

//go:embed all:dist
var embedded embed.FS

// Handler serves the embedded application and supports dependency injection for tests.
func Handler() http.Handler {
	assets, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic("embedded web assets unavailable: " + err.Error())
	}
	return NewHandler(assets)
}

// NewHandler serves files from assets and falls back to index.html for client routes.
func NewHandler(assets fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reservedPath(r.URL.Path) {
			respond.Error(w, http.StatusNotFound, "not_found", "route not found")
			return
		}

		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "." {
			name = "index.html"
		}
		if info, err := fs.Stat(assets, name); err == nil && !info.IsDir() {
			serveFile(w, r, assets, name)
			return
		}
		serveFile(w, r, assets, "index.html")
	})
}

func reservedPath(requestPath string) bool {
	for _, prefix := range []string{"/api/", "/auth/", "/hooks/", "/healthz"} {
		if strings.HasPrefix(requestPath, prefix) {
			return true
		}
	}
	return false
}

func serveFile(w http.ResponseWriter, r *http.Request, assets fs.FS, name string) {
	if strings.HasPrefix(name, "_app/immutable/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else if name == "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeFileFS(w, r, assets, name)
}
