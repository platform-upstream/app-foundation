package access

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInternalAdmitsEachCallerByItsKey(t *testing.T) {
	finance, omni := strings.Repeat("f", 64), strings.Repeat("o", 32)
	var seen []string
	guard := Internal(map[string]string{"finance": finance, "omni": omni}, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, ok := InternalCallerFrom(r.Context())
		if !ok {
			t.Fatal("no caller in the context")
		}
		seen = append(seen, name)
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, c := range []struct{ header, want string }{
		{"Bearer " + finance, "finance"},
		{"bearer " + omni, "omni"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/internal/x", nil)
		r.RemoteAddr = "10.42.0.9:40000"
		r.Header.Set("Authorization", c.header)
		w := httptest.NewRecorder()
		guard.ServeHTTP(w, r)
		if w.Code != http.StatusNoContent {
			t.Fatalf("%s: status %d", c.want, w.Code)
		}
	}
	if strings.Join(seen, ",") != "finance,omni" {
		t.Fatalf("callers %v", seen)
	}
}

func TestInternalRefusesBadKeysAndForwardedRequests(t *testing.T) {
	key := strings.Repeat("k", 40)
	var got []error
	reached := false
	guard := Internal(map[string]string{"finance": key}, func(w http.ResponseWriter, _ *http.Request, err error) {
		got = append(got, err)
		w.WriteHeader(http.StatusTeapot)
	})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	call := func(headers http.Header) int {
		r := httptest.NewRequest(http.MethodPost, "/internal/intake", nil)
		for name, values := range headers {
			for _, v := range values {
				r.Header.Add(name, v)
			}
		}
		w := httptest.NewRecorder()
		guard.ServeHTTP(w, r)
		return w.Code
	}
	badKeys := map[string]http.Header{
		"no key":             {},
		"empty bearer":       {"Authorization": {"Bearer "}},
		"raw key":            {"Authorization": {key}},
		"other scheme":       {"Authorization": {"Basic " + key}},
		"wrong key":          {"Authorization": {"Bearer " + strings.Repeat("x", 40)}},
		"prefix of the key":  {"Authorization": {"Bearer " + key[:39]}},
		"key with extra":     {"Authorization": {"Bearer " + key + "x"}},
		"two headers":        {"Authorization": {"Bearer " + key, "Bearer " + key}},
		"key and whitespace": {"Authorization": {"Bearer  " + key}},
	}
	for name, headers := range badKeys {
		got = nil
		if code := call(headers); code != http.StatusTeapot || len(got) != 1 || !errors.Is(got[0], ErrInvalidKey) {
			t.Errorf("%s: status %d, errors %v", name, code, got)
		}
	}
	for _, header := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-Ip", "Cf-Connecting-Ip", "Cf-Ray", "Cf-Access-Jwt-Assertion"} {
		for _, value := range []string{"203.0.113.7", ""} {
			got = nil
			if code := call(http.Header{"Authorization": {"Bearer " + key}, http.CanonicalHeaderKey(header): {value}}); code != http.StatusTeapot || len(got) != 1 || !errors.Is(got[0], ErrForwarded) {
				t.Errorf("%s=%q: status %d, errors %v", header, value, code, got)
			}
		}
	}
	if reached {
		t.Fatal("a refused request reached the handler")
	}
	if code := call(http.Header{"Authorization": {"Bearer " + key}}); code != http.StatusOK || !reached {
		t.Fatalf("the right key was refused: %d", code)
	}
}

func TestInternalWithoutUsableKeyIsClosed(t *testing.T) {
	guard := Internal(map[string]string{"finance": "", "omni": strings.Repeat("s", MinInternalKeyLength-1), "": strings.Repeat("n", 40)}, nil)(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("reached the handler") }))
	for _, header := range []string{"Bearer ", "Bearer " + strings.Repeat("s", MinInternalKeyLength-1), "Bearer " + strings.Repeat("n", 40)} {
		r := httptest.NewRequest(http.MethodGet, "/internal/x", nil)
		r.Header.Set("Authorization", header)
		w := httptest.NewRecorder()
		guard.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized || w.Body.String() != "{\"error\":\"Unauthorized.\"}\n" || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("%q: %d %q", header, w.Code, w.Body)
		}
	}
	if _, ok := InternalCallerFrom(httptest.NewRequest(http.MethodGet, "/", nil).Context()); ok {
		t.Fatal("caller outside the guard")
	}
}

func TestRemoteAdmitsForwardedKeyAndKeepsItsOwnCaller(t *testing.T) {
	key := strings.Repeat("r", 32)
	var internal, remote bool
	guard := Remote(map[string]string{"agent-x": key, "short": "s"}, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, internal = InternalCallerFrom(r.Context())
		var name string
		name, remote = RemoteCallerFrom(r.Context())
		if name != "agent-x" {
			t.Errorf("caller %q", name)
		}
	}))
	for _, c := range []struct {
		header string
		status int
	}{{"Bearer " + key, 200}, {"Bearer s", 401}, {"", 401}} {
		r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		r.Header.Set("Cf-Ray", "8f00")
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
		if c.header != "" {
			r.Header.Set("Authorization", c.header)
		}
		w := httptest.NewRecorder()
		guard.ServeHTTP(w, r)
		if w.Code != c.status {
			t.Errorf("%q: status %d, want %d", c.header, w.Code, c.status)
		}
	}
	if internal || !remote {
		t.Errorf("internal %v remote %v, want only remote", internal, remote)
	}
}
