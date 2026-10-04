package reference

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/niiku-platform/app-foundation/access"
	"github.com/niiku-platform/app-foundation/httpx"
	"github.com/niiku-platform/app-foundation/memory"
	"github.com/niiku-platform/app-foundation/reference-app/migrations"
	"github.com/niiku-platform/app-foundation/server"
)

var testAssets = fstest.MapFS{
	"index.html":           {Data: []byte(`<!doctype html><div id="root">reference shell</div>`)},
	"assets/app.js":        {Data: []byte(`console.log("reference")`)},
	"sw.js":                {Data: []byte(`self.addEventListener("fetch",()=>{})`)},
	"manifest.webmanifest": {Data: []byte(`{"name":"Reference"}`)},
}

func TestHTTPAuthRoutingAndJSONBoundaries(t *testing.T) {
	handler, closeHandler := newTestHandler(t, access.Config{Mode: access.ModeLocal, Listener: "127.0.0.1:8099"}, nil, nil)
	defer closeHandler()

	tests := []struct {
		name              string
		method            string
		path              string
		body              string
		contentType       string
		origin            string
		peer              string
		status            int
		contentTypePrefix string
	}{
		{name: "public shallow health", method: http.MethodGet, path: "/healthz", peer: "192.0.2.1:1234", status: 200, contentTypePrefix: "application/json"},
		{name: "embedded shell", method: http.MethodGet, path: "/", peer: "127.0.0.1:1234", status: 200, contentTypePrefix: "text/html"},
		{name: "client route fallback", method: http.MethodGet, path: "/settings/profile", peer: "127.0.0.1:1234", status: 200, contentTypePrefix: "text/html"},
		{name: "missing file", method: http.MethodGet, path: "/missing.js", peer: "127.0.0.1:1234", status: 404, contentTypePrefix: "text/plain"},
		{name: "API boundary", method: http.MethodGet, path: "/api/missing", peer: "127.0.0.1:1234", status: 404, contentTypePrefix: "application/json"},
		{name: "media type", method: http.MethodPost, path: "/api/items", body: `{}`, peer: "127.0.0.1:1234", status: 415, contentTypePrefix: "application/json"},
		{name: "unknown field", method: http.MethodPost, path: "/api/items", body: `{"name":"x","extra":true}`, contentType: "application/json", peer: "127.0.0.1:1234", status: 400, contentTypePrefix: "application/json"},
		{name: "cross site", method: http.MethodPost, path: "/api/items", body: `{"name":"x"}`, contentType: "application/json", origin: "https://evil.invalid", peer: "127.0.0.1:1234", status: 403, contentTypePrefix: "application/json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "http://reference.test"+test.path, strings.NewReader(test.body))
			request.RemoteAddr = test.peer
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
			}
			if !strings.HasPrefix(response.Header().Get("Content-Type"), test.contentTypePrefix) {
				t.Fatalf("content type = %q", response.Header().Get("Content-Type"))
			}
			if response.Header().Get(httpx.RequestIDHeader) == "" {
				t.Fatal("missing request ID")
			}
			if response.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("missing CSP")
			}
			if test.status >= 400 && strings.HasPrefix(test.contentTypePrefix, "application/json") {
				var envelope struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Error == "" {
					t.Fatalf("error envelope = %#v, %v", envelope, err)
				}
			}
		})
	}

	large := `{"name":"` + strings.Repeat("x", int(httpx.DefaultJSONBodyLimit)) + `"}`
	request := httptest.NewRequest(http.MethodPost, "http://reference.test/api/items", strings.NewReader(large))
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large body status = %d", response.Code)
	}
}

func TestStateMutationAndDisabledIntegrationFallbacks(t *testing.T) {
	handler, closeHandler := newTestHandler(t, access.Config{Mode: access.ModeLocal, Listener: "127.0.0.1:8099"}, nil, nil)
	defer closeHandler()

	created := doRequest(t, handler, http.MethodPost, "/api/items", `{"name":"temporary proof"}`, "application/json", "http://reference.test")
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body %s", created.Code, created.Body.String())
	}
	state := doRequest(t, handler, http.MethodGet, "/api/state", "", "", "")
	var payload struct {
		Items []Item `json:"items"`
	}
	if err := json.Unmarshal(state.Body.Bytes(), &payload); err != nil || len(payload.Items) != 1 || payload.Items[0].Name != "temporary proof" {
		t.Fatalf("state = %#v, %v", payload, err)
	}
	for _, route := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/memory/people", ""},
		{http.MethodPost, "/api/complete", `{"prompt":"probe"}`},
	} {
		response := doRequest(t, handler, route.method, route.path, route.body, "application/json", "http://reference.test")
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d", route.path, response.Code)
		}
	}
}

func TestCloudflareModeProtectsShellButNotHealth(t *testing.T) {
	handler, closeHandler := newTestHandler(t, access.Config{
		Mode: access.ModeCloudflare, TeamDomain: "reference.cloudflareaccess.com", Audience: "reference-audience",
	}, nil, nil)
	defer closeHandler()
	for _, test := range []struct {
		path   string
		status int
	}{{"/healthz", 200}, {"/", 401}, {"/api/state", 401}} {
		request := httptest.NewRequest(http.MethodGet, "https://reference.test"+test.path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("%s status = %d", test.path, response.Code)
		}
	}
}

func TestMemoryAndLLMSeamsUseFakeServers(t *testing.T) {
	memoryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/people" || r.Header.Get("Authorization") != "Bearer memory-key" {
			t.Errorf("Memory request = %s, auth %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"people":[{"id":"owner","name":"Owner"}]}`))
	}))
	defer memoryServer.Close()
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer llm-key" {
			t.Errorf("LLM auth = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"fake seam result"}}]}`))
	}))
	defer llmServer.Close()

	db, err := OpenStore(context.Background(), filepath.Join(t.TempDir(), "seams.sqlite"), migrations.Files)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	memoryClient, err := memory.New(db, memory.Config{BaseURL: memoryServer.URL, APIKey: "memory-key", App: "reference", HTTPClient: memoryServer.Client()})
	if err != nil {
		t.Fatal(err)
	}
	authenticator, err := access.New(access.Config{Mode: access.ModeLocal, Listener: "127.0.0.1:8099"})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(HandlerConfig{
		Database: db, Assets: testAssets, Authenticator: authenticator, AllowedOrigins: []string{"http://reference.test"},
		Memory: memoryClient, LLM: NewLLMClient(llmServer.URL, "llm-key", llmServer.Client()),
	})
	if err != nil {
		t.Fatal(err)
	}
	people := doRequest(t, handler, http.MethodGet, "/api/memory/people", "", "", "")
	if people.Code != 200 || !strings.Contains(people.Body.String(), `"name":"Owner"`) {
		t.Fatalf("people = %d %s", people.Code, people.Body.String())
	}
	completion := doRequest(t, handler, http.MethodPost, "/api/complete", `{"prompt":"probe"}`, "application/json", "http://reference.test")
	if completion.Code != 200 || !strings.Contains(completion.Body.String(), "fake seam result") {
		t.Fatalf("completion = %d %s", completion.Code, completion.Body.String())
	}
}

func TestGracefulShutdownCompletesInflightRequest(t *testing.T) {
	handler, closeHandler := newTestHandler(t, access.Config{Mode: access.ModeLocal, Listener: "127.0.0.1:8099"}, nil, nil)
	defer closeHandler()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	var once sync.Once
	observed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/wait" {
			once.Do(func() { close(started) })
		}
		handler.ServeHTTP(w, r)
	})
	ctx, cancel := context.WithCancel(context.Background())
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(ctx, server.New(listener.Addr().String(), observed), listener) }()

	request, err := http.NewRequest(http.MethodPost, "http://"+listener.Addr().String()+"/api/wait", bytes.NewBufferString(`{"milliseconds":250}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://"+listener.Addr().String())
	responseResult := make(chan *http.Response, 1)
	responseError := make(chan error, 1)
	go func() {
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			responseError <- err
			return
		}
		responseResult <- response
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-responseError:
		t.Fatal(err)
	case response := <-responseResult:
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode != 200 || !strings.Contains(string(body), `"completed":true`) {
			t.Fatalf("response = %d %s", response.StatusCode, body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight request did not complete")
	}
	select {
	case err := <-serveResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop")
	}
}

func newTestHandler(t *testing.T, authConfig access.Config, memoryClient MemoryClient, llm CompletionClient) (http.Handler, func()) {
	t.Helper()
	db, err := OpenStore(context.Background(), filepath.Join(t.TempDir(), "reference.sqlite"), migrations.Files)
	if err != nil {
		t.Fatal(err)
	}
	if memoryClient == nil {
		memoryClient, err = memory.New(db, memory.Config{App: "reference"})
		if err != nil {
			t.Fatal(err)
		}
	}
	if llm == nil {
		llm = NewLLMClient("", "", nil)
	}
	authenticator, err := access.New(authConfig)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(HandlerConfig{
		Database: db, Assets: testAssets, Authenticator: authenticator,
		AllowedOrigins: []string{"http://reference.test"}, Memory: memoryClient, LLM: llm,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler, func() { _ = db.Close() }
}

func doRequest(t *testing.T, handler http.Handler, method, path, body, contentType, origin string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "http://reference.test"+path, strings.NewReader(body))
	request.RemoteAddr = "127.0.0.1:1234"
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
