package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSON(t *testing.T) {
	type input struct {
		Name string `json:"name"`
	}
	tests := []struct {
		name, contentType, body string
		limit                   int64
		want                    error
		status                  int
	}{
		{"valid", "application/json; charset=utf-8", `{"name":"read"}`, 100, nil, 0},
		{"wrong content type", "text/plain", `{}`, 100, ErrUnsupportedMediaType, 415},
		{"unknown field", "application/json", `{"extra":true}`, 100, ErrInvalidJSON, 400},
		{"trailing value", "application/json", `{"name":"read"}{}`, 100, ErrInvalidJSON, 400},
		{"too large", "application/json", `{"name":"read"}`, 4, ErrBodyTooLarge, 413},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			err := DecodeJSON(httptest.NewRecorder(), r, &input{}, tc.limit)
			if !errors.Is(err, tc.want) {
				t.Fatalf("DecodeJSON error = %v, want %v", err, tc.want)
			}
			if tc.want != nil && StatusCode(err) != tc.status {
				t.Fatalf("StatusCode = %d, want %d", StatusCode(err), tc.status)
			}
		})
	}
}

func TestRequestIDs(t *testing.T) {
	h := RequestIDs(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := RequestID(r.Context()); got == "" || got != w.Header().Get(RequestIDHeader) {
			t.Errorf("context/header IDs = %q/%q", got, w.Header().Get(RequestIDHeader))
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, tc := range []struct{ supplied, want string }{
		{"edge-request_123", "edge-request_123"},
		{"contains spaces", ""},
		{strings.Repeat("a", 129), ""},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set(RequestIDHeader, tc.supplied)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		got := w.Header().Get(RequestIDHeader)
		if tc.want != "" && got != tc.want {
			t.Errorf("request ID = %q, want %q", got, tc.want)
		}
		if tc.want == "" && (got == "" || got == tc.supplied) {
			t.Errorf("unsafe request ID was not replaced: %q", got)
		}
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	if err := WriteError(w, http.StatusConflict, "changed"); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusConflict || w.Header().Get("Content-Type") != "application/json; charset=utf-8" || w.Body.String() != "{\"error\":\"changed\"}\n" {
		t.Fatalf("response = %d %q %q", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
}

func TestSameOrigin(t *testing.T) {
	handler := SameOrigin([]string{"https://read.niiku.xyz", "https://code.niiku.xyz"}, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, tc := range []struct {
		name, method, origin, fetchSite string
		want                            int
	}{
		{"read bypass", http.MethodGet, "https://evil.example", "cross-site", 204},
		{"production", http.MethodPost, "https://read.niiku.xyz", "same-origin", 204},
		{"development proxy", http.MethodPost, "https://code.niiku.xyz", "same-site", 204},
		{"direct loopback", http.MethodPost, "http://127.0.0.1:8096", "same-origin", 204},
		{"cross site", http.MethodPost, "https://evil.example", "cross-site", 403},
		{"origin mismatch", http.MethodPost, "https://evil.example", "same-site", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "http://127.0.0.1:8096/api/items", nil)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%q", w.Code, tc.want, w.Body.String())
			}
		})
	}
}
