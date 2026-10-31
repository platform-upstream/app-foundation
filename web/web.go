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

// PublicPrefix holds the files a home screen fetches without the Cloudflare Access cookie: the web
// app manifest and the icons. iOS requests the touch icon and the manifest without cookies, so
// behind Access they come back as a sign-in redirect and the home screen shows a letter instead of
// the icon. The edge lets exactly this prefix past Access on every app host (platform/.opentofu,
// pwa_public), and PublicFiles lets it past the app's own authentication. Put nothing here that
// is not meant for everyone: no data, no HTML.
const PublicPrefix = "/pwa/"

// Public reports whether a request is a GET or HEAD for a file under PublicPrefix. The path is
// cleaned first, so "/pwa/../api/x" is not public.
//
// Do not build an authentication bypass on Public: compose with PublicFiles, which is the only
// safe composition. Public says nothing about which handler answers; a gate such as
// "if web.Public(r) { mux.ServeHTTP(w, r) } else { auth(mux).ServeHTTP(w, r) }" lets every
// /pwa/<x>.<ext> request reach the whole mux without a session: an app route registered under
// /pwa/, a GET handler with a wildcard pattern, or a mux that rewrites paths would then answer
// unauthenticated. PublicFiles sends public requests to the static file handler only.
func Public(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	cleaned := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
	return strings.HasPrefix(cleaned, PublicPrefix) && path.Ext(cleaned) != ""
}

// PublicFiles sends Public requests to files (normally the Handler) without authentication and
// every other request to protected (the application behind its Access middleware). Public
// requests never reach the application's API. It is the supported way to serve PublicPrefix; pass
// the static file handler as files, never the application mux (see the package example).
func PublicFiles(files, protected http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if Public(r) {
			files.ServeHTTP(w, r)
			return
		}
		protected.ServeHTTP(w, r)
	})
}

// Handler validates index.html and returns a static handler with SPA fallback. Vite assets under
// /assets are immutable; the service worker, the manifest and PublicPrefix files revalidate;
// documents are never stored. A missing file or a directory under /assets or PublicPrefix is a
// 404, never the SPA shell.
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
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		cleaned := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		if cleaned == "/api" || strings.HasPrefix(cleaned, "/api/") || cleaned == "/internal" || strings.HasPrefix(cleaned, "/internal/") {
			notFound(w, r)
			return
		}
		relative := strings.TrimPrefix(cleaned, "/")
		if relative == "" || relative == "index.html" {
			serveIndex(w, r, index)
			return
		}

		file, err := fsys.Open(relative)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				internalError(w)
				return
			}
			if staticOnly(cleaned) || path.Ext(cleaned) != "" {
				notFound(w, r)
				return
			}
			serveIndex(w, r, index)
			return
		}
		info, statErr := file.Stat()
		_ = file.Close()
		if statErr != nil {
			internalError(w)
			return
		}
		if info.IsDir() {
			if staticOnly(cleaned) {
				notFound(w, r)
				return
			}
			serveIndex(w, r, index)
			return
		}

		switch {
		case strings.HasPrefix(cleaned, "/assets/"):
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		case cleaned == "/sw.js" || cleaned == "/manifest.webmanifest" || strings.HasPrefix(cleaned, PublicPrefix):
			w.Header().Set("Cache-Control", "no-cache")
		default:
			w.Header().Set("Cache-Control", "no-store")
		}
		clone := r.Clone(r.Context())
		clone.URL.Path = cleaned
		clone.URL.RawPath = ""
		files.ServeHTTP(w, clone)
	}), nil
}

// staticOnly reports whether a path lies in a directory that holds files only, where a miss is a
// 404 and never falls back to the SPA shell.
func staticOnly(cleaned string) bool {
	return strings.HasPrefix(cleaned+"/", "/assets/") || strings.HasPrefix(cleaned+"/", PublicPrefix)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	http.NotFound(w, r)
}

func internalError(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "Internal Server Error", http.StatusInternalServerError)
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
