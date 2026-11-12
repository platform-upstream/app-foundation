package llm

import (
	"encoding/json"
	"errors"
	"strings"
)

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
		Usage   *wireUsage      `json:"usage"`
		Timings json.RawMessage `json:"timings"`
	}
	if err := json.Unmarshal(data, &completion); err != nil || len(completion.Choices) == 0 {
		return Response{}, errors.New("llm: invalid reply")
	}
	choice := completion.Choices[0]
	resp := Response{FinishReason: choice.FinishReason, Timings: completion.Timings}
	if completion.Usage != nil {
		resp.Usage = completion.Usage.usage()
	}
	if len(resp.Timings) == 0 || string(resp.Timings) == "null" {
		resp.Timings = nil
	}
	resp.Content, resp.Reasoning = splitThinking(contentText(choice.Message.Content), choice.Message.ReasoningContent+choice.Message.Reasoning)
	for _, call := range choice.Message.ToolCalls {
		resp.ToolCalls = append(resp.ToolCalls, ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments, Extra: call.Extra})
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
	Index    int             `json:"index"`
	ID       string          `json:"id"`
	Extra    json.RawMessage `json:"extra_content"`
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

// wireUsage reads the cached-input count from either place backends put it.
type wireUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CachedTokens     int `json:"cached_tokens"`
	Details          *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

func (u wireUsage) usage() Usage {
	cached := u.CachedTokens
	if u.Details != nil && u.Details.CachedTokens > 0 {
		cached = u.Details.CachedTokens
	}
	return Usage{PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens, TotalTokens: u.TotalTokens, CachedTokens: cached}
}
