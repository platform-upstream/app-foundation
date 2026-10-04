// Package web serves an embedded single-page application without caching authenticated HTML.
package web

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
)

func init() {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// Handler validates index.html and returns a static handler with SPA fallback. Vite assets under
// /assets are immutable; the service worker and manifest revalidate; documents are never stored.
func Handler(fsys fs.FS) (http.Handler, error) {
	if fsys == nil {
		return nil, errors.New("web filesystem is nil")
	}
	index, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		return nil, errors.New("web filesystem has no index.html")
	}
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		cleaned := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		if cleaned == "/api" || strings.HasPrefix(cleaned, "/api/") || cleaned == "/internal" || strings.HasPrefix(cleaned, "/internal/") {
			http.NotFound(w, r)
			return
		}
		relative := strings.TrimPrefix(cleaned, "/")
		if relative == "" || relative == "index.html" {
			serveIndex(w, r, index)
			return
		}

		file, err := fsys.Open(relative)
		if err != nil {
			if strings.HasPrefix(cleaned, "/assets/") || path.Ext(cleaned) != "" {
				http.NotFound(w, r)
				return
			}
			serveIndex(w, r, index)
			return
		}
		info, statErr := file.Stat()
		_ = file.Close()
		if statErr != nil || info.IsDir() {
			if strings.HasPrefix(cleaned, "/assets/") {
				http.NotFound(w, r)
				return
			}
			serveIndex(w, r, index)
			return
		}

		switch {
		case strings.HasPrefix(cleaned, "/assets/"):
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		case cleaned == "/sw.js" || cleaned == "/manifest.webmanifest":
			w.Header().Set("Cache-Control", "no-cache")
		default:
			w.Header().Set("Cache-Control", "no-store")
		}
		clone := r.Clone(r.Context())
		clone.URL.Path = cleaned
		files.ServeHTTP(w, clone)
	}), nil
}

func serveIndex(w http.ResponseWriter, r *http.Request, index []byte) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(index)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(index)
	}
}
