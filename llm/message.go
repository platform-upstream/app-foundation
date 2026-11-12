package llm

import (
	"encoding/json"
	"errors"
)

// Message is one chat message: Content, or Parts for text mixed with images (and documents, see
// File). An assistant
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
	// Extra is the call's extra_content as the backend sent it (Gemini 3 keeps its thought
	// signature there and rejects or degrades a replayed call without it); sent back unchanged.
	Extra json.RawMessage
}

func (t Tool) MarshalJSON() ([]byte, error) {
	parameters := t.Parameters
	if len(parameters) == 0 {
		parameters = json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return json.Marshal(map[string]any{"type": "function", "function": map[string]any{
		"name": t.Name, "description": t.Description, "parameters": parameters,
	}})
}

func (c ToolCall) MarshalJSON() ([]byte, error) {
	out := map[string]any{"id": c.ID, "type": "function", "function": map[string]string{
		"name": c.Name, "arguments": c.Arguments,
	}}
	if len(c.Extra) > 0 && string(c.Extra) != "null" {
		out["extra_content"] = c.Extra
	}
	return json.Marshal(out)
}

// Part is a piece of a multi-part message.
type Part struct {
	kind, text, url, name string
}

// Text is a text part.
func Text(text string) Part { return Part{kind: "text", text: text} }

// Image is an image part; url is usually a data URI (data:image/png;base64,...).
func Image(url string) Part { return Part{kind: "image_url", url: url} }

// File is a document part (OpenAI "file", the bytes as a data URI). It is not portable: through
// the gateway Claude reads PDF and text files, other types fail there, and the local runtime
// rejects every file part. Prefer sending extracted text where the app can.
func File(name, dataURI string) Part { return Part{kind: "file", name: name, url: dataURI} }

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
		case "file":
			parts = append(parts, map[string]any{"type": "file", "file": map[string]string{"filename": p.name, "file_data": p.url}})
		default:
			return nil, errors.New("llm: empty message part")
		}
	}
	out["content"] = parts
	return json.Marshal(out)
}
