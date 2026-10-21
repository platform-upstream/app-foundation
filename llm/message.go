package llm

import (
	"encoding/json"
	"errors"
)

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
