package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func headersFor(t *testing.T, policy HeaderPolicy) http.Header {
	t.Helper()
	handler := Headers(policy)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("next not called: %d", rec.Code)
	}
	return rec.Header()
}

func TestHeadersDefaults(t *testing.T) {
	got := headersFor(t, HeaderPolicy{})
	for name, want := range map[string]string{
		"Content-Security-Policy":    DefaultCSP,
		"X-Content-Type-Options":     "nosniff",
		"Referrer-Policy":            "same-origin",
		"X-Frame-Options":            "DENY",
		"Cross-Origin-Opener-Policy": "same-origin",
		"Strict-Transport-Security":  "max-age=31536000",
		"Permissions-Policy":         DefaultPermissionsPolicy,
	} {
		if got.Get(name) != want {
			t.Errorf("%s = %q, want %q", name, got.Get(name), want)
		}
	}
}

func TestHeadersPolicy(t *testing.T) {
	got := headersFor(t, HeaderPolicy{FrameOptions: "sameorigin", NoHSTS: true, PermissionsPolicy: "camera=(self)"})
	if got.Get("X-Frame-Options") != "SAMEORIGIN" || got.Get("Strict-Transport-Security") != "" || got.Get("Permissions-Policy") != "camera=(self)" {
		t.Fatalf("policy not applied: %v", got)
	}
	if want := "default-src 'self'; img-src 'self' data: blob:; object-src 'none'; base-uri 'self'; frame-ancestors 'self'"; got.Get("Content-Security-Policy") != want {
		t.Fatalf("CSP = %q", got.Get("Content-Security-Policy"))
	}
	own := "default-src 'self'; script-src 'self' https://cdn.example.org"
	if got := headersFor(t, HeaderPolicy{CSP: own}); got.Get("Content-Security-Policy") != own {
		t.Fatalf("app CSP replaced: %q", got.Get("Content-Security-Policy"))
	}
}

func TestHeadersRejectsInvalidPolicy(t *testing.T) {
	for _, policy := range []HeaderPolicy{
		{FrameOptions: "ALLOW-FROM https://example.org"},
		{CSP: "default-src 'self'\r\nSet-Cookie: x=1"},
		{PermissionsPolicy: "camera=()\n"},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("accepted %+v", policy)
				}
			}()
			Headers(policy)
		}()
	}
}
