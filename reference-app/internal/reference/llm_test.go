package reference

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLLMClientAgainstFakeServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("request = %s, auth %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model != "local-llm" || len(body.Messages) != 1 || body.Messages[0].Content != "probe" {
			t.Errorf("body = %#v", body)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"fake completion"}}]}`))
	}))
	defer server.Close()
	client := NewLLMClient(server.URL, "test-key", server.Client())
	value, err := client.Complete(context.Background(), "probe")
	if err != nil || value != "fake completion" {
		t.Fatalf("Complete = %q, %v", value, err)
	}
}

func TestLLMClientBoundsAndValidation(t *testing.T) {
	if NewLLMClient("", "", nil).Enabled() {
		t.Fatal("empty client enabled")
	}
	provided := &http.Client{Timeout: time.Minute}
	if got := NewLLMClient("https://example.invalid", "key", provided).http.Timeout; got != 5*time.Second {
		t.Fatalf("timeout = %s", got)
	}
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "missing choice", body: `{"choices":[]}`},
		{name: "trailing value", body: `{"choices":[{"message":{"content":"x"}}]} {}`},
		{name: "too large", body: strings.Repeat("x", int(maxLLMResponseBytes)+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(test.body)) }))
			defer server.Close()
			if _, err := NewLLMClient(server.URL, "key", server.Client()).Complete(context.Background(), "probe"); err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
}
