package llm_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/castillo-platform/app-foundation/llm"
	"github.com/castillo-platform/app-foundation/llm/llmtest"
)

func client(t *testing.T, server *llmtest.Server) *llm.Client {
	t.Helper()
	c, err := llm.New(llm.Config{BaseURL: server.URL, APIKey: "test-key", Model: "app", UserAgent: "App/1.0"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRequestShape(t *testing.T) {
	server := llmtest.NewServer(t, func(llmtest.Request) llmtest.Reply { return llmtest.Reply{Content: "Answer"} })
	c := client(t, server)
	if _, err := c.Complete(context.Background(), llm.Request{
		Messages:  []llm.Message{{Role: "system", Content: "Be brief."}, {Role: "user", Content: "Hi"}},
		MaxTokens: 800,
	}); err != nil {
		t.Fatal(err)
	}
	warm := 0.3
	if _, err := c.Complete(context.Background(), llm.Request{
		Messages:    []llm.Message{{Role: "user", Parts: []llm.Part{llm.Text("What is this?"), llm.Image("data:image/png;base64,AAAA"), llm.File("a.pdf", "data:application/pdf;base64,JVBE")}}},
		MaxTokens:   100,
		Effort:      llm.EffortLow,
		Temperature: &warm,
	}); err != nil {
		t.Fatal(err)
	}
	requests := server.Requests()
	first, second := requests[0], requests[1]
	if first.Header.Get("Authorization") != "Bearer test-key" || first.Header.Get("User-Agent") != "App/1.0" {
		t.Fatalf("headers = %v", first.Header)
	}
	if first.Body["model"] != "app" || first.Body["max_tokens"] != float64(800) || first.Body["reasoning_effort"] != "none" {
		t.Fatalf("body = %s", first.Raw)
	}
	if _, ok := first.Body["temperature"]; ok {
		t.Fatalf("temperature sent without being set: %s", first.Raw)
	}
	if second.Body["reasoning_effort"] != "low" || second.Body["temperature"] != 0.3 {
		t.Fatalf("body = %s", second.Raw)
	}
	if !strings.Contains(string(second.Raw), `"content":[{"text":"What is this?","type":"text"},{"image_url":{"url":"data:image/png;base64,AAAA"},"type":"image_url"},{"file":{"file_data":"data:application/pdf;base64,JVBE","filename":"a.pdf"},"type":"file"}]`) {
		t.Fatalf("parts = %s", second.Raw)
	}
}

func TestConfiguration(t *testing.T) {
	disabled, err := llm.New(llm.Config{BaseURL: "https://gateway.invalid/v1"})
	if err != nil || disabled.Enabled() {
		t.Fatalf("no key: enabled=%v err=%v", disabled.Enabled(), err)
	}
	if _, err := disabled.Complete(context.Background(), llm.Request{}); !errors.Is(err, llm.ErrDisabled) {
		t.Fatalf("disabled call: %v", err)
	}
	if _, err := llm.New(llm.Config{BaseURL: "https://gateway.invalid/v1", APIKey: "k"}); err == nil {
		t.Fatal("a key without a model alias was accepted")
	}
	if _, err := llm.New(llm.Config{BaseURL: "gateway/v1", APIKey: "k", Model: "app"}); err == nil {
		t.Fatal("a relative base URL was accepted")
	}
	server := llmtest.NewServer(t, func(llmtest.Request) llmtest.Reply { return llmtest.Reply{Content: "x"} })
	c := client(t, server)
	for _, req := range []llm.Request{
		{MaxTokens: 10},
		{Messages: []llm.Message{{Role: "user", Content: "Hi"}}},
		{Messages: []llm.Message{{Role: "user", Content: "Hi"}}, MaxTokens: 10, Effort: "minimal"},
	} {
		if _, err := c.Complete(context.Background(), req); err == nil {
			t.Fatalf("invalid request accepted: %+v", req)
		}
	}
	if len(server.Requests()) != 0 {
		t.Fatal("an invalid request reached the gateway")
	}
}

func TestReplies(t *testing.T) {
	cases := []struct {
		name               string
		reply              llmtest.Reply
		content, reasoning string
		err                error
		status             int
	}{
		{"reasoning field", llmtest.Reply{Content: " Answer ", Reasoning: "Because."}, "Answer", "Because.", nil, 0},
		{"think tags", llmtest.Reply{Content: "<think>Because.</think>\nAnswer"}, "Answer", "Because.", nil, 0},
		{"closing tag only", llmtest.Reply{Content: "Because.</think>Answer"}, "Answer", "Because.", nil, 0},
		{"content parts", llmtest.Reply{Raw: `{"choices":[{"message":{"content":[{"type":"text","text":"An"},{"type":"text","text":"swer"}]},"finish_reason":"stop"}]}`}, "Answer", "", nil, 0},
		{"truncated", llmtest.Reply{Content: "Half", FinishReason: "length"}, "Half", "", llm.ErrTruncated, 0},
		{"empty", llmtest.Reply{Content: "<think>Only thought.</think>"}, "", "Only thought.", llm.ErrEmpty, 0},
		{"gateway error", llmtest.Reply{Status: 400, Raw: `{"error":{"message":"temperature is deprecated for this model","type":"invalid_request_error"}}`}, "", "", nil, 400},
		{"plain error", llmtest.Reply{Status: 502, Raw: "bad gateway"}, "", "", nil, 502},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := llmtest.NewServer(t, func(llmtest.Request) llmtest.Reply { return tc.reply })
			resp, err := client(t, server).Complete(context.Background(), llm.Request{Messages: []llm.Message{{Role: "user", Content: "Hi"}}, MaxTokens: 10})
			if tc.status != 0 {
				var gateway *llm.Error
				if !errors.As(err, &gateway) || gateway.Status != tc.status || gateway.Message == "" {
					t.Fatalf("err = %v, want HTTP %d with a message", err, tc.status)
				}
				return
			}
			if !errors.Is(err, tc.err) || (tc.err == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if resp.Content != tc.content || resp.Reasoning != tc.reasoning {
				t.Fatalf("content %q reasoning %q", resp.Content, resp.Reasoning)
			}
		})
	}
}

func TestUsageAndTimings(t *testing.T) {
	server := llmtest.NewServer(t, func(llmtest.Request) llmtest.Reply {
		return llmtest.Reply{Raw: `{"choices":[{"message":{"content":"x"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5},"timings":{"predicted_n":2}}`}
	})
	resp, err := client(t, server).Complete(context.Background(), llm.Request{Messages: []llm.Message{{Role: "user", Content: "Hi"}}, MaxTokens: 10})
	if err != nil || resp.Usage != (llm.Usage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5}) || string(resp.Timings) != `{"predicted_n":2}` {
		t.Fatalf("resp = %+v, err = %v", resp, err)
	}
}

func TestCompleteJSON(t *testing.T) {
	type answer struct {
		Items []string `json:"items"`
	}
	schema := llm.Schema{Name: "items", Schema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["items"],"properties":{"items":{"type":"array","items":{"type":"string"}}}}`)}
	for _, content := range []string{
		`{"items":["a","b"]}`,
		"```json\n{\"items\":[\"a\",\"b\"]}\n```",
		"Here it is: {\"items\":[\"a\",\"b\"]} done",
	} {
		server := llmtest.NewServer(t, func(llmtest.Request) llmtest.Reply { return llmtest.Reply{Content: content} })
		got, _, err := llm.CompleteJSON[answer](context.Background(), client(t, server), llm.Request{Messages: []llm.Message{{Role: "user", Content: "List"}}, MaxTokens: 50}, schema)
		if err != nil || strings.Join(got.Items, ",") != "a,b" {
			t.Fatalf("content %q: got %+v, err %v", content, got, err)
		}
		format, _ := server.Requests()[0].Body["response_format"].(map[string]any)
		inner, _ := format["json_schema"].(map[string]any)
		if format["type"] != "json_schema" || inner["name"] != "items" || inner["strict"] != true || inner["schema"] == nil {
			t.Fatalf("response_format = %v", format)
		}
	}
	server := llmtest.NewServer(t, func(llmtest.Request) llmtest.Reply { return llmtest.Reply{Content: "not json"} })
	if _, _, err := llm.CompleteJSON[answer](context.Background(), client(t, server), llm.Request{Messages: []llm.Message{{Role: "user", Content: "List"}}, MaxTokens: 50}, schema); !errors.Is(err, llm.ErrMalformed) {
		t.Fatalf("err = %v, want ErrMalformed", err)
	}
	if _, _, err := llm.CompleteJSON[answer](context.Background(), client(t, server), llm.Request{Messages: []llm.Message{{Role: "user", Content: "List"}}, MaxTokens: 50}, llm.Schema{Name: "x", Schema: json.RawMessage(`{`)}); err == nil {
		t.Fatal("an invalid schema was accepted")
	}
}

func TestDeadlineIsTheCallersContext(t *testing.T) {
	server := llmtest.NewServer(t, func(llmtest.Request) llmtest.Reply { return llmtest.Reply{Content: "x"} })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client(t, server).Complete(ctx, llm.Request{Messages: []llm.Message{{Role: "user", Content: "Hi"}}, MaxTokens: 10}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestCachedTokens(t *testing.T) {
	for _, usage := range []string{
		`{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"prompt_tokens_details":{"cached_tokens":8}}`,
		`{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"cached_tokens":8}`,
	} {
		server := llmtest.NewServer(t, func(llmtest.Request) llmtest.Reply {
			return llmtest.Reply{Raw: `{"choices":[{"message":{"content":"x"},"finish_reason":"stop"}],"usage":` + usage + `}`}
		})
		resp, err := client(t, server).Complete(context.Background(), llm.Request{Messages: []llm.Message{{Role: "user", Content: "Hi"}}, MaxTokens: 10})
		if err != nil || resp.Usage.CachedTokens != 8 || resp.Usage.PromptTokens != 10 {
			t.Fatalf("usage %s: %+v %v", usage, resp.Usage, err)
		}
	}
}
