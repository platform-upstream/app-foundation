package llm_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/niiku-platform/app-foundation/llm"
	"github.com/niiku-platform/app-foundation/llm/llmtest"
)

func stream(t *testing.T, chunks []string, req llm.Request) (llm.Response, []llm.Event, error, llmtest.Request) {
	t.Helper()
	server := llmtest.NewServer(t, func(llmtest.Request) llmtest.Reply { return llmtest.Reply{Stream: chunks} })
	var events []llm.Event
	if req.Messages == nil {
		req.Messages = []llm.Message{{Role: "user", Content: "Hi"}}
	}
	if req.MaxTokens == 0 {
		req.MaxTokens = 100
	}
	resp, err := client(t, server).Stream(context.Background(), req, func(e llm.Event) { events = append(events, e) })
	return resp, events, err, server.Requests()[0]
}

func TestStreamTextAndThinking(t *testing.T) {
	resp, events, err, sent := stream(t, []string{
		`{"choices":[{"delta":{"reasoning_content":"Plan. "}}]}`,
		`{"choices":[{"delta":{"content":"<thi"}}]}`,
		`{"choices":[{"delta":{"content":"nk>more</thi"}}]}`,
		`{"choices":[{"delta":{"content":"nk>Ans"}}]}`,
		`{"choices":[{"delta":{"content":"wer"},"finish_reason":"stop"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":6,"total_tokens":10},"timings":{"predicted_n":6}}`,
		`[DONE]`,
	}, llm.Request{})
	if err != nil || resp.Content != "Answer" || resp.Reasoning != "Plan. more" || resp.FinishReason != "stop" {
		t.Fatalf("resp = %+v, err = %v", resp, err)
	}
	if resp.Usage.TotalTokens != 10 || string(resp.Timings) != `{"predicted_n":6}` {
		t.Fatalf("usage %+v timings %s", resp.Usage, resp.Timings)
	}
	var text, thought strings.Builder
	for _, e := range events {
		switch e.Kind {
		case llm.EventText:
			text.WriteString(e.Text)
		case llm.EventReasoning:
			thought.WriteString(e.Text)
		}
	}
	if text.String() != "Answer" || thought.String() != "Plan. more" {
		t.Fatalf("events: text %q reasoning %q", text.String(), thought.String())
	}
	opts, _ := sent.Body["stream_options"].(map[string]any)
	if sent.Body["stream"] != true || opts["include_usage"] != true || sent.Body["reasoning_effort"] != "none" {
		t.Fatalf("body = %s", sent.Raw)
	}
}

func TestStreamToolCalls(t *testing.T) {
	tool := llm.Tool{Name: "web_search", Description: "Search.", Parameters: []byte(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`)}
	resp, events, err, sent := stream(t, []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"web_","arguments":"{\"query\":\""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"search","arguments":"news\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`[DONE]`,
	}, llm.Request{
		Messages: []llm.Message{
			{Role: "user", Content: "News?"},
			{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-0", Name: "web_search", Arguments: `{"query":"old"}`}}},
			{Role: "tool", ToolCallID: "call-0", Content: "Result"},
		},
		Tools: []llm.Tool{tool},
	})
	if err != nil || len(resp.ToolCalls) != 1 || resp.ToolCalls[0] != (llm.ToolCall{ID: "call-1", Name: "web_search", Arguments: `{"query":"news"}`}) {
		t.Fatalf("resp = %+v, err = %v", resp, err)
	}
	if len(events) != 2 || events[0].Kind != llm.EventToolCall {
		t.Fatalf("events = %+v", events)
	}
	raw := string(sent.Raw)
	for _, want := range []string{
		`"tool_choice":"auto"`,
		`"tools":[{"function":{"description":"Search.","name":"web_search","parameters":{"type":"object"`,
		`"tool_calls":[{"function":{"arguments":"{\"query\":\"old\"}","name":"web_search"},"id":"call-0","type":"function"}]`,
		`"tool_call_id":"call-0"`,
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("body lacks %s:\n%s", want, raw)
		}
	}
}

func TestStreamEndings(t *testing.T) {
	if resp, _, err, _ := stream(t, []string{`{"choices":[{"delta":{"content":"Half"}}]}`}, llm.Request{}); !errors.Is(err, llm.ErrIncomplete) || resp.Content != "Half" {
		t.Fatalf("no end: %+v %v", resp, err)
	}
	if resp, _, err, _ := stream(t, []string{`{"choices":[{"delta":{"content":"Half"},"finish_reason":"length"}]}`, `[DONE]`}, llm.Request{}); !errors.Is(err, llm.ErrTruncated) || resp.Content != "Half" {
		t.Fatalf("length: %+v %v", resp, err)
	}
	if _, _, err, _ := stream(t, []string{`{"error":{"message":"model overloaded"}}`}, llm.Request{}); err == nil || !strings.Contains(err.Error(), "model overloaded") {
		t.Fatalf("error chunk: %v", err)
	}
	if _, _, err, _ := stream(t, []string{`{"choices":[{"delta":{},"finish_reason":"stop"}]}`, `[DONE]`}, llm.Request{}); !errors.Is(err, llm.ErrEmpty) {
		t.Fatalf("empty: %v", err)
	}
}

func TestStreamPlainJSONReply(t *testing.T) {
	server := llmtest.NewServer(t, func(llmtest.Request) llmtest.Reply { return llmtest.Reply{Content: "Whole", Reasoning: "Thought"} })
	var events []llm.Event
	resp, err := client(t, server).Stream(context.Background(), llm.Request{Messages: []llm.Message{{Role: "user", Content: "Hi"}}, MaxTokens: 10}, func(e llm.Event) { events = append(events, e) })
	if err != nil || resp.Content != "Whole" || len(events) != 2 || events[1].Text != "Whole" {
		t.Fatalf("resp %+v events %+v err %v", resp, events, err)
	}
}

func TestStreamGatewayError(t *testing.T) {
	server := llmtest.NewServer(t, func(llmtest.Request) llmtest.Reply {
		return llmtest.Reply{Status: 503, Raw: `{"error":{"message":"model is loading"}}`}
	})
	_, err := client(t, server).Stream(context.Background(), llm.Request{Messages: []llm.Message{{Role: "user", Content: "Hi"}}, MaxTokens: 10}, nil)
	var gateway *llm.Error
	if !errors.As(err, &gateway) || gateway.Status != 503 || gateway.Message != "model is loading" {
		t.Fatalf("err = %v", err)
	}
}
