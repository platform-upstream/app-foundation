package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Schema is a named JSON schema for structured output. It is sent in strict mode, so every
// property must be listed in required and objects need "additionalProperties": false.
type Schema struct {
	Name   string
	Schema json.RawMessage
}

// CompleteJSON asks for a reply that matches the schema and decodes it into T. Both backends
// constrain the output to the schema; the decode still tolerates code fences and text around the
// JSON. Field-level validation stays with the caller.
func CompleteJSON[T any](ctx context.Context, c *Client, req Request, schema Schema) (T, Response, error) {
	var value T
	if schema.Name == "" || !json.Valid(schema.Schema) {
		return value, Response{}, errors.New("llm: a schema needs a name and valid JSON")
	}
	req.format = map[string]any{"type": "json_schema", "json_schema": map[string]any{
		"name": schema.Name, "strict": true, "schema": schema.Schema,
	}}
	resp, err := c.Complete(ctx, req)
	if err != nil {
		return value, resp, err
	}
	if err := json.Unmarshal([]byte(jsonText(resp.Content)), &value); err != nil {
		return value, resp, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	return value, resp, nil
}

// jsonText cuts the JSON value out of a reply: inside a ```json fence when there is one, then
// from the first opening brace or bracket to the last matching closing one.
func jsonText(content string) string {
	if start := strings.Index(content, "```"); start >= 0 {
		rest := content[start+3:]
		if newline := strings.IndexByte(rest, '\n'); newline >= 0 {
			rest = rest[newline+1:]
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			content = rest[:end]
		}
	}
	open := strings.IndexAny(content, "{[")
	if open < 0 {
		return strings.TrimSpace(content)
	}
	closer := "}"
	if content[open] == '[' {
		closer = "]"
	}
	if end := strings.LastIndex(content, closer); end > open {
		return content[open : end+1]
	}
	return strings.TrimSpace(content[open:])
}
