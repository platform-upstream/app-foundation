package access_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/platform-upstream/app-foundation/access"
	"github.com/platform-upstream/app-foundation/httpx"
)

// An app reads its configuration from the environment the chart sets, gates everything behind
// Access except the probes and the internal routes, and reads the caller from the context. The
// example runs in local mode; in production AUTH_MODE is unset and CF_TEAM_DOMAIN and
// CF_ACCESS_AUD are required.
func Example() {
	os.Setenv(access.EnvMode, "local")
	defer os.Unsetenv(access.EnvMode)

	cfg, err := access.ConfigFromEnv("127.0.0.1:8080")
	if err != nil {
		panic(err) // a misconfigured app does not start
	}
	auth, err := access.New(cfg)
	if err != nil {
		panic(err)
	}

	api := http.NewServeMux()
	api.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		caller, _ := access.CallerFrom(r.Context())
		// Routes that must be a person (not a service token) check caller.Person().
		fmt.Fprintf(w, "local=%v person=%v", caller.Local, caller.Person())
	})
	gated := auth.Middleware(func(w http.ResponseWriter, _ *http.Request, _ error) {
		_ = httpx.WriteError(w, http.StatusUnauthorized, "Please sign in again.") // app-owned copy
	})(api)

	for _, peer := range []string{"127.0.0.1:50000", "203.0.113.7:50000"} {
		request := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		request.RemoteAddr = peer
		response := httptest.NewRecorder()
		gated.ServeHTTP(response, request)
		fmt.Println(response.Code, strings.TrimSpace(response.Body.String()))
	}
	// Output:
	// 200 local=true person=false
	// 401 {"error":"Please sign in again."}
}

// Internal routes are served outside Access for sibling apps in the cluster, one bearer key per
// caller (at least 32 bytes; an unset key closes the route). Requests that came through the
// ingress or the tunnel are refused.
func ExampleInternal() {
	keys := map[string]string{"mail-bridge": strings.Repeat("k", 32)} // in an app: os.Getenv(...)
	intake := access.Internal(keys, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, _ := access.InternalCallerFrom(r.Context())
		fmt.Fprint(w, "from ", caller)
	}))

	direct := httptest.NewRequest(http.MethodPost, "/internal/intake", nil)
	direct.Header.Set("Authorization", "Bearer "+keys["mail-bridge"])
	viaIngress := direct.Clone(direct.Context())
	viaIngress.Header.Set("X-Forwarded-For", "203.0.113.7")
	for _, request := range []*http.Request{direct, viaIngress} {
		response := httptest.NewRecorder()
		intake.ServeHTTP(response, request)
		fmt.Println(response.Code, strings.TrimSpace(response.Body.String()))
	}
	// Output:
	// 200 from mail-bridge
	// 401 {"error":"Unauthorized."}
}
