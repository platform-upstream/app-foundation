package agentkit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearer struct {
	key  string
	next http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.key)
	return b.next.RoundTrip(r)
}

type item struct {
	Kind string `json:"kind"`
}

type toolIn struct {
	Format string `json:"format,omitempty"`
	Items  []item `json:"items,omitempty"`
	Nested struct {
		Mode string `json:"mode,omitempty"`
	} `json:"nested,omitzero"`
}

type toolOut struct {
	Refs []string `json:"refs"`
}

var errMistake = errors.New("mistake")

// session serves a server with the test tools behind Handler and connects to it as caller.
func session(t *testing.T, caller string) *mcp.ClientSession {
	t.Helper()
	keys := map[string]string{"reader": key, "editor": strings.Repeat("e", 32)}
	callers := Callers{"reader": Read, "editor": Edit}
	server := mcp.NewServer(&mcp.Implementation{Name: "acme", Version: "1"}, nil)
	tools := Tools{
		Server:  server,
		Callers: callers,
		Fail: func(tool string, err error) error {
			if errors.Is(err, errMistake) {
				return err
			}
			return errors.New(tool + " could not answer")
		},
		Links: func(out any) []string { return out.(toolOut).Refs },
	}
	AddTool(tools, Tool{Name: "acme_things_read", Class: Read, Idempotent: true}, "Reads.",
		map[string][]any{"format": {"concise", "detailed"}, "items.kind": {"a", "b"}, "nested.mode": {"x"}},
		func(_ context.Context, req *mcp.CallToolRequest, in toolIn) (string, toolOut, error) {
			run, _ := RunOf(req)
			return "1 thing.", toolOut{Refs: []string{"acme:thing/1", "acme:caller/" + run.Caller}}, nil
		})
	AddTool(tools, Tool{Name: "acme_things_edit", Class: Edit, Destructive: true}, "Edits.", nil,
		func(context.Context, *mcp.CallToolRequest, struct{}) (string, toolOut, error) {
			return "", toolOut{}, errors.New("disk on fire")
		})
	AddTool(tools, Tool{Name: "acme_things_check", Class: Read}, "Checks.", nil,
		func(context.Context, *mcp.CallToolRequest, struct{}) (string, toolOut, error) {
			return "", toolOut{}, errMistake
		})
	srv := httptest.NewServer(Handler(keys, nil, callers, time.Minute, func(*http.Request) *mcp.Server { return server }))
	t.Cleanup(srv.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: srv.URL, HTTPClient: &http.Client{Transport: bearer{keys[caller], http.DefaultTransport}}}
	s, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func text(result *mcp.CallToolResult) string {
	return result.Content[0].(*mcp.TextContent).Text
}

func TestAddToolListsSchemaAndHints(t *testing.T) {
	s := session(t, "reader")
	list, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*mcp.Tool{}
	for _, tool := range list.Tools {
		byName[tool.Name] = tool
	}
	read, edit := byName["acme_things_read"], byName["acme_things_edit"]
	if read == nil || edit == nil {
		t.Fatalf("tools = %v", list.Tools)
	}
	a := read.Annotations
	if !a.ReadOnlyHint || !a.IdempotentHint || a.DestructiveHint != nil || a.OpenWorldHint == nil || *a.OpenWorldHint {
		t.Errorf("read hints = %+v", a)
	}
	a = edit.Annotations
	if a.ReadOnlyHint || a.IdempotentHint || a.DestructiveHint == nil || !*a.DestructiveHint {
		t.Errorf("edit hints = %+v", a)
	}
	schema, _ := json.Marshal(read.InputSchema)
	for _, want := range []string{`"enum":["concise","detailed"]`, `"enum":["a","b"]`, `"enum":["x"]`} {
		if !strings.Contains(string(schema), want) {
			t.Errorf("schema %s lacks %s", schema, want)
		}
	}
}

func TestAddToolAnswers(t *testing.T) {
	ctx := context.Background()
	s := session(t, "reader")
	result, err := s.CallTool(ctx, &mcp.CallToolParams{Name: "acme_things_read", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("read = %v, %v", text(result), err)
	}
	if got := text(result); got != "1 thing.\n"+`{"refs":["acme:thing/1","acme:caller/reader"]}` {
		t.Errorf("text = %q", got)
	}
	if len(result.Content) != 3 || result.Content[2].(*mcp.ResourceLink).URI != "acme:caller/reader" {
		t.Errorf("links = %v", result.Content)
	}

	result, err = s.CallTool(ctx, &mcp.CallToolParams{Name: "acme_things_edit", Arguments: map[string]any{}})
	if err != nil || !result.IsError || !strings.Contains(text(result), "not allowed for caller reader") {
		t.Errorf("edit above the ceiling = %v, %v", result, err)
	}
	result, err = s.CallTool(ctx, &mcp.CallToolParams{Name: "acme_things_check", Arguments: map[string]any{}})
	if err != nil || !result.IsError || text(result) != "mistake" {
		t.Errorf("caller's mistake = %v, %v", result, err)
	}

	s = session(t, "editor")
	result, err = s.CallTool(ctx, &mcp.CallToolParams{Name: "acme_things_edit", Arguments: map[string]any{}})
	if err != nil || !result.IsError || text(result) != "acme_things_edit could not answer" {
		t.Errorf("internal error = %v, %v", result, err)
	}
}

func TestHandlerGuardsAndNoStore(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "acme", Version: "1"}, nil)
	h := Handler(map[string]string{"reader": key}, nil, Callers{"reader": Read}, time.Minute, func(*http.Request) *mcp.Server { return server })
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/mcp", strings.NewReader("{}")))
	if w.Code != http.StatusUnauthorized || w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("without a key: %d, Cache-Control %q", w.Code, w.Header().Get("Cache-Control"))
	}
}

func TestInputSchemaUnknownFieldPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "no field items.colour") {
			t.Errorf("recover = %v", r)
		}
	}()
	InputSchema[toolIn]("acme_things_read", map[string][]any{"items.colour": {"red"}})
}
