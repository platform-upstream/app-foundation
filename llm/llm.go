// Package llm calls the LLM gateway's OpenAI-compatible chat completions API with one portable
// request shape, so the same call works whichever model the gateway serves under the app's alias.
// A request carries only fields every backend reads: reasoning effort instead of a backend's own
// thinking switch, an explicit output ceiling, structured output as a strict JSON schema, and
// sampling only as a best-effort hint. Prompts, schemas, ceilings, deadlines, concurrency and
// validation stay with the application.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// DefaultMaxResponseBytes bounds a reply body when Config.MaxResponseBytes is not set.
const DefaultMaxResponseBytes = 4 << 20

var (
	// ErrDisabled is returned by every call of a client built without an API key.
	ErrDisabled = errors.New("llm: no API key configured")
	// ErrTruncated means the reply stopped at MaxTokens; the partial Response is returned with it.
	ErrTruncated = errors.New("llm: reply reached the output limit")
	// ErrEmpty means the model finished without any reply text.
	ErrEmpty = errors.New("llm: empty reply")
	// ErrMalformed means a CompleteJSON reply did not decode into the requested type.
	ErrMalformed = errors.New("llm: reply is not the requested JSON")
)

// Error is a non-2xx answer from the gateway. Message is the gateway's error text, bounded.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("llm: HTTP %d", e.Status)
	}
	return fmt.Sprintf("llm: HTTP %d: %s", e.Status, e.Message)
}

// Config describes one gateway consumer.
type Config struct {
	// BaseURL is the API root including /v1, e.g. https://llm-api.int.niiku.xyz/v1.
	BaseURL string
	// APIKey is the consumer's gateway key. Without one the client is disabled: Enabled reports
	// false and calls return ErrDisabled, so the application can run without inference.
	APIKey string
	// Model is the consumer's gateway alias. It is required together with an APIKey.
	Model string
	// HTTP is optional. The default sends no proxy and follows no redirects. Deadlines come from
	// the caller's context; a Client.Timeout would also cut long replies.
	HTTP             *http.Client
	UserAgent        string
	MaxResponseBytes int64
}

// Client is safe for concurrent use.
type Client struct {
	cfg      Config
	endpoint string
	http     *http.Client
}

// New validates the configuration. An APIKey without a Model is an error, so a missing alias
// fails at startup instead of reaching a model by accident.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return &Client{cfg: cfg}, nil
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("llm: an API key needs a model alias")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("llm: base URL must be an absolute HTTP(S) URL")
	}
	client := cfg.HTTP
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		client = &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	if cfg.MaxResponseBytes <= 0 {
		cfg.MaxResponseBytes = DefaultMaxResponseBytes
	}
	return &Client{cfg: cfg, endpoint: strings.TrimRight(cfg.BaseURL, "/") + "/chat/completions", http: client}, nil
}

// Enabled reports whether the client has an API key.
func (c *Client) Enabled() bool { return c != nil && c.http != nil }

// Effort is the reasoning effort. The zero value is sent as "none": no thinking. Backends differ
// in what an omitted effort means, so a request always carries one.
type Effort string

const (
	EffortNone   Effort = "none"
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortXHigh  Effort = "xhigh"
	EffortMax    Effort = "max"
)

func (e Effort) wire() (string, error) {
	switch e {
	case "":
		return string(EffortNone), nil
	case EffortNone, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax:
		return string(e), nil
	}
	return "", fmt.Errorf("llm: unknown effort %q", string(e))
}

// Message is one chat message: Content, or Parts for text mixed with images. An assistant
// message that called tools carries ToolCalls; the tool's answer is a "tool" message with
// ToolCallID.
type Message struct {
	Role       string
	Content    string
	Parts      []Part
	ToolCalls  []ToolCall
	ToolCallID string
}

// Tool is a function the model may call. Parameters is its JSON schema.
type Tool struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ToolCall is one call the model made; Arguments is the JSON text the model wrote.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

func (t Tool) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"type": "function", "function": map[string]any{
		"name": t.Name, "description": t.Description, "parameters": t.Parameters,
	}})
}

func (c ToolCall) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"id": c.ID, "type": "function", "function": map[string]string{
		"name": c.Name, "arguments": c.Arguments,
	}})
}

// Part is a piece of a multi-part message.
type Part struct {
	kind, text, url string
}

// Text is a text part.
func Text(text string) Part { return Part{kind: "text", text: text} }

// Image is an image part; url is usually a data URI (data:image/png;base64,...).
func Image(url string) Part { return Part{kind: "image_url", url: url} }

func (m Message) MarshalJSON() ([]byte, error) {
	out := map[string]any{"role": m.Role, "content": m.Content}
	if len(m.ToolCalls) > 0 {
		out["tool_calls"] = m.ToolCalls
	}
	if m.ToolCallID != "" {
		out["tool_call_id"] = m.ToolCallID
	}
	if len(m.Parts) == 0 {
		return json.Marshal(out)
	}
	parts := make([]map[string]any, 0, len(m.Parts))
	for _, p := range m.Parts {
		switch p.kind {
		case "text":
			parts = append(parts, map[string]any{"type": "text", "text": p.text})
		case "image_url":
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]string{"url": p.url}})
		default:
			return nil, errors.New("llm: empty message part")
		}
	}
	out["content"] = parts
	return json.Marshal(out)
}

// Request is one completion.
type Request struct {
	Messages []Message
	// MaxTokens is required: a ceiling for thinking and answer together, not a length target.
	MaxTokens int
	Effort    Effort
	// Temperature is a best-effort hint: the local runtime honours it, Claude drops it. A prompt
	// must work without it.
	Temperature *float64
	// Tools are offered with tool_choice "auto".
	Tools []Tool

	format any
}

// Usage is the token count the gateway reported.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Response is a finished reply.
type Response struct {
	// Content is the answer with any thinking removed.
	Content string
	// Reasoning is the thinking text when the backend returned it.
	Reasoning    string
	ToolCalls    []ToolCall
	FinishReason string
	Usage        Usage
	// Timings is the local runtime's speed report; nil from backends that send none.
	Timings json.RawMessage
}

// Complete sends a request and waits for the whole reply. The deadline is ctx's.
func (c *Client) Complete(ctx context.Context, req Request) (Response, error) {
	httpResp, err := c.send(ctx, req, false)
	if err != nil {
		return Response{}, err
	}
	defer httpResp.Body.Close()
	data, err := c.readAll(httpResp.Body)
	if err != nil {
		return Response{}, err
	}
	return decode(data)
}

// send posts the request and returns a 2xx response; any other status becomes an *Error.
func (c *Client) send(ctx context.Context, req Request, stream bool) (*http.Response, error) {
	if !c.Enabled() {
		return nil, ErrDisabled
	}
	body, err := c.body(req, stream)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	if c.cfg.UserAgent != "" {
		httpReq.Header.Set("User-Agent", c.cfg.UserAgent)
	}
	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		defer httpResp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(httpResp.Body, 64<<10))
		return nil, &Error{Status: httpResp.StatusCode, Message: errorMessage(data)}
	}
	return httpResp, nil
}

func (c *Client) readAll(body io.Reader) ([]byte, error) {
	limit := c.cfg.MaxResponseBytes
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, errors.New("llm: reply is too large")
	}
	return data, nil
}

func (c *Client) body(req Request, stream bool) ([]byte, error) {
	if len(req.Messages) == 0 {
		return nil, errors.New("llm: a request needs messages")
	}
	if req.MaxTokens <= 0 {
		return nil, errors.New("llm: a request needs MaxTokens")
	}
	effort, err := req.Effort.wire()
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"model":            c.cfg.Model,
		"messages":         req.Messages,
		"max_tokens":       req.MaxTokens,
		"reasoning_effort": effort,
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.format != nil {
		body["response_format"] = req.format
	}
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
		body["tool_choice"] = "auto"
	}
	if stream {
		body["stream"] = true
		body["stream_options"] = map[string]bool{"include_usage": true}
	}
	return json.Marshal(body)
}

func errorMessage(data []byte) string {
	var envelope struct {
		Error json.RawMessage `json:"error"`
	}
	text := strings.TrimSpace(string(data))
	if json.Unmarshal(data, &envelope) == nil && len(envelope.Error) > 0 {
		var detail struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(envelope.Error, &detail) == nil && detail.Message != "" {
			text = detail.Message
		} else if json.Unmarshal(envelope.Error, &text) != nil {
			text = string(envelope.Error)
		}
	}
	if runes := []rune(text); len(runes) > 240 {
		text = string(runes[:240])
	}
	return text
}

func decode(data []byte) (Response, error) {
	var completion struct {
		Choices []struct {
			Message struct {
				Content          json.RawMessage `json:"content"`
				ReasoningContent string          `json:"reasoning_content"`
				Reasoning        string          `json:"reasoning"`
				ToolCalls        []wireToolCall  `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage   *Usage          `json:"usage"`
		Timings json.RawMessage `json:"timings"`
	}
	if err := json.Unmarshal(data, &completion); err != nil || len(completion.Choices) == 0 {
		return Response{}, errors.New("llm: invalid reply")
	}
	choice := completion.Choices[0]
	resp := Response{FinishReason: choice.FinishReason, Timings: completion.Timings}
	if completion.Usage != nil {
		resp.Usage = *completion.Usage
	}
	if len(resp.Timings) == 0 || string(resp.Timings) == "null" {
		resp.Timings = nil
	}
	resp.Content, resp.Reasoning = splitThinking(contentText(choice.Message.Content), choice.Message.ReasoningContent+choice.Message.Reasoning)
	for _, call := range choice.Message.ToolCalls {
		resp.ToolCalls = append(resp.ToolCalls, ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
	}
	return finish(resp)
}

// finish turns the outcome of a complete reply into its error, if any.
func finish(resp Response) (Response, error) {
	if resp.FinishReason == "length" {
		return resp, ErrTruncated
	}
	if resp.Content == "" && len(resp.ToolCalls) == 0 {
		return resp, ErrEmpty
	}
	return resp, nil
}

type wireToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// contentText reads content as a string or as an array of text parts.
func contentText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var joined strings.Builder
		for _, part := range parts {
			if part.Type == "text" {
				joined.WriteString(part.Text)
			}
		}
		return joined.String()
	}
	return ""
}

// splitThinking moves a <think>…</think> block that a backend left in the content into the
// reasoning, unless the backend already returned the reasoning separately.
func splitThinking(content, reasoning string) (string, string) {
	if end := strings.LastIndex(content, "</think>"); end >= 0 {
		thought := strings.TrimSpace(content[:end])
		thought = strings.TrimSpace(strings.TrimPrefix(thought, "<think>"))
		if reasoning == "" {
			reasoning = thought
		}
		content = content[end+len("</think>"):]
	}
	return strings.TrimSpace(content), strings.TrimSpace(reasoning)
}
