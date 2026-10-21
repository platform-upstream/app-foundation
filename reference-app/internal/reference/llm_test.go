package reference

import (
	"context"
	"errors"
	"testing"

	"github.com/niiku-platform/app-foundation/llm"
	"github.com/niiku-platform/app-foundation/llm/llmtest"
)

func TestLLMClientAgainstFakeServer(t *testing.T) {
	server := llmtest.NewServer(t, func(llmtest.Request) llmtest.Reply { return llmtest.Reply{Content: "fake completion"} })
	client, err := NewLLMClient(llm.Config{BaseURL: server.URL, APIKey: "test-key", Model: "reference"})
	if err != nil {
		t.Fatal(err)
	}
	value, err := client.Complete(context.Background(), "probe")
	if err != nil || value != "fake completion" {
		t.Fatalf("Complete = %q, %v", value, err)
	}
	sent := server.Requests()[0]
	if sent.Header.Get("Authorization") != "Bearer test-key" || sent.Body["model"] != "reference" || sent.Body["reasoning_effort"] != "none" || sent.Body["max_tokens"] != float64(1000) {
		t.Fatalf("request = %s", sent.Raw)
	}
}

func TestLLMClientConfiguration(t *testing.T) {
	disabled, err := NewLLMClient(llm.Config{})
	if err != nil || disabled.Enabled() {
		t.Fatalf("no key: enabled=%v err=%v", disabled.Enabled(), err)
	}
	if _, err := disabled.Complete(context.Background(), "probe"); !errors.Is(err, ErrLLMNotConfigured) {
		t.Fatalf("disabled Complete = %v", err)
	}
	if _, err := NewLLMClient(llm.Config{BaseURL: "https://example.invalid/v1", APIKey: "key"}); err == nil {
		t.Fatal("a key without a model alias was accepted")
	}
	server := llmtest.NewServer(t, func(llmtest.Request) llmtest.Reply { return llmtest.Reply{Content: ""} })
	client, _ := NewLLMClient(llm.Config{BaseURL: server.URL, APIKey: "key", Model: "reference"})
	if _, err := client.Complete(context.Background(), "probe"); !errors.Is(err, llm.ErrEmpty) {
		t.Fatalf("empty reply accepted: %v", err)
	}
}
