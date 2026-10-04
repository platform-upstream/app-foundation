package web

import (
	"io"
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
		{"/assets/missing.js", "", "404", 404},
		{"/api/missing", "", "404", 404},
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
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST response = %d %#v", w.Code, w.Header())
	}
}
