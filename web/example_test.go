package web_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing/fstest"

	"github.com/platform-upstream/app-foundation/access"
	"github.com/platform-upstream/app-foundation/server"
	"github.com/platform-upstream/app-foundation/web"
)

// The canonical composition of an app's routes, outermost first: security headers, the public
// probes, PublicFiles (the /pwa/ files straight from the static handler), then authentication in
// front of the API and the SPA. web.Public on its own must never decide whether the whole mux runs
// without authentication.
func Example() {
	// In an app: the embedded Vite build (fs.Sub of an embed.FS).
	files := fstest.MapFS{
		"index.html":       {Data: []byte("<!doctype html><div id=root></div>")},
		"pwa/icon-180.png": {Data: []byte("png")},
	}
	spa, err := web.Handler(files)
	if err != nil {
		panic(err)
	}
	// In an app: access.ConfigFromEnv(addr). Local mode admits loopback peers only.
	auth, err := access.New(access.Config{Mode: access.ModeLocal, Listener: "127.0.0.1:8080"})
	if err != nil {
		panic(err)
	}

	protected := http.NewServeMux()
	protected.Handle("GET /api/notes", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("[]"))
	}))
	// A mistake PublicFiles contains: an app route under /pwa/ is never reached without a session.
	protected.Handle("GET /pwa/export.json", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("private data"))
	}))
	protected.Handle("/", spa)

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", server.Health())
	mux.Handle("GET /readyz", server.Ready( /* db.PingContext */ ))
	mux.Handle("/", web.PublicFiles(spa, auth.Middleware(nil)(protected)))
	handler := web.Headers(web.HeaderPolicy{})(mux)

	for _, path := range []string{"/healthz", "/pwa/icon-180.png", "/", "/api/notes", "/pwa/export.json"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.RemoteAddr = "203.0.113.7:4000" // not signed in, not loopback
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		fmt.Println(path, response.Code)
	}
	// Output:
	// /healthz 200
	// /pwa/icon-180.png 200
	// / 401
	// /api/notes 401
	// /pwa/export.json 404
}
