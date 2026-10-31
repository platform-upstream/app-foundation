package web

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHandlerServingAndCaching(t *testing.T) {
	handler, err := Handler(fstest.MapFS{
		"index.html":           {Data: []byte("<html>Read</html>")},
		"assets/app.abc123.js": {Data: []byte("app()")},
		"sw.js":                {Data: []byte("worker()")},
		"manifest.webmanifest": {Data: []byte(`{"name":"Read"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path, cache, contains string
		status                int
	}{
		{"/", "no-store", "Read", 200},
		{"/library/item", "no-store", "Read", 200},
		{"/assets/app.abc123.js", "public, max-age=31536000, immutable", "app()", 200},
		{"/sw.js", "no-cache", "worker()", 200},
		{"/manifest.webmanifest", "no-cache", "Read", 200},
		{"/assets/missing.js", "no-store", "404", 404},
		{"/missing.json", "no-store", "404", 404},
		{"/api/missing", "no-store", "404", 404},
		{"/internal/missing", "no-store", "404", 404},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			result := w.Result()
			body, _ := io.ReadAll(result.Body)
			if result.StatusCode != tc.status || !strings.Contains(string(body), tc.contains) || result.Header.Get("Cache-Control") != tc.cache {
				t.Fatalf("response = %d cache=%q body=%q", result.StatusCode, result.Header.Get("Cache-Control"), body)
			}
		})
	}
}

func TestHandlerRequiresIndexAndReadMethod(t *testing.T) {
	if _, err := Handler(fstest.MapFS{}); err == nil {
		t.Fatal("Handler accepted filesystem without index.html")
	}
	handler, err := Handler(fstest.MapFS{"index.html": {Data: []byte("index")}})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/", nil))
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, HEAD" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("POST response = %d %#v", w.Code, w.Header())
	}
}

func TestHandlerCanonicalizesEncodedAssetPath(t *testing.T) {
	handler, err := Handler(fstest.MapFS{
		"index.html":           {Data: []byte("index")},
		"assets/app.abc123.js": {Data: []byte("app()")},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "http://example.test/assets%2Fapp.abc123.js", nil)
	if r.URL.RawPath == "" {
		t.Fatal("test request did not retain an encoded raw path")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Body.String() != "app()" || w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("response = %d cache=%q body=%q", w.Code, w.Header().Get("Cache-Control"), w.Body.String())
	}
}

type errorFS struct{}

func (errorFS) Open(name string) (fs.File, error) {
	if name == "index.html" {
		return fstest.MapFS{"index.html": {Data: []byte("index")}}.Open(name)
	}
	return nil, errors.New("storage failed")
}

func TestHandlerDoesNotTurnFilesystemErrorsIntoSPAResponses(t *testing.T) {
	handler, err := Handler(errorFS{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/client-route", nil))
	if w.Code != http.StatusInternalServerError || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response = %d cache=%q body=%q", w.Code, w.Header().Get("Cache-Control"), w.Body.String())
	}
}

func TestPublicPrefix(t *testing.T) {
	handler, err := Handler(fstest.MapFS{
		"index.html":               {Data: []byte("<html>App</html>")},
		"pwa/manifest.webmanifest": {Data: []byte(`{"name":"App"}`)},
		"pwa/icon-180.png":         {Data: []byte("png")},
		"pwa/nested/readme.txt":    {Data: []byte("text")},
	})
	if err != nil {
		t.Fatal(err)
	}
	protected := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	app := PublicFiles(handler, protected)
	tests := []struct {
		method, path string
		status       int
		cache        string
	}{
		{http.MethodGet, "/pwa/manifest.webmanifest", 200, "no-cache"},
		{http.MethodHead, "/pwa/icon-180.png", 200, "no-cache"},
		{http.MethodGet, "/pwa/missing.png", 404, "no-store"},
		{http.MethodGet, "/pwa/nested/readme.txt", 200, "no-cache"},
		// Directories and extensionless paths never reach the files unauthenticated.
		{http.MethodGet, "/pwa/", 401, ""},
		{http.MethodGet, "/pwa/nested", 401, ""},
		// Cleaning happens before the decision.
		{http.MethodGet, "/pwa/../api/items.json", 401, ""},
		{http.MethodGet, "/pwa/%2e%2e/index.html", 401, ""},
		{http.MethodPost, "/pwa/icon-180.png", 401, ""},
		{http.MethodGet, "/", 401, ""},
		{http.MethodGet, "/pwafake/x.png", 401, ""},
	}
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			app.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status || w.Header().Get("Cache-Control") != tc.cache {
				t.Fatalf("status=%d cache=%q, want %d %q", w.Code, w.Header().Get("Cache-Control"), tc.status, tc.cache)
			}
		})
	}
	// Behind authentication, a directory under the prefix is a 404, not the SPA shell.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pwa/nested", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("directory under prefix = %d, want 404", w.Code)
	}
}
