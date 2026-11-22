package agentkit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool is one MCP tool: its name, its class (the control) and the hints that only describe it.
type Tool struct {
	Name  string
	Class Class
	// Destructive is sent only above read; a read tool is read-only and so never destructive.
	Destructive bool
	Idempotent  bool
}

// Annotations are t's hints: read-only for Read, destructive (above read) and idempotent as set,
// and never open world, since an app's tools reach only the app's own data.
func (t Tool) Annotations() *mcp.ToolAnnotations {
	closed := false
	annotations := &mcp.ToolAnnotations{ReadOnlyHint: t.Class == Read, IdempotentHint: t.Idempotent, OpenWorldHint: &closed}
	if t.Class != Read {
		destructive := t.Destructive
		annotations.DestructiveHint = &destructive
	}
	return annotations
}

// InputSchema is In's JSON schema (jsonschema.For) with the allowed values of some fields. A
// dotted field walks into a list's items or an object's own fields at any depth: "facts.person" is
// the person of each item of facts. A field In does not have panics, naming tool: it is a mistake
// in the app's code, seen at start.
func InputSchema[In any](tool string, enums map[string][]any) *jsonschema.Schema {
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("tool %s: %v", tool, err))
	}
	for field, values := range enums {
		property := schema
		for _, name := range strings.Split(field, ".") {
			if property.Items != nil {
				property = property.Items
			}
			if property = property.Properties[name]; property == nil {
				panic(fmt.Sprintf("tool %s: no field %s", tool, field))
			}
		}
		property.Enum = values
	}
	return schema
}

// Tools is the server an app registers its tools on and the app's own rules for answers.
type Tools struct {
	Server  *mcp.Server
	Callers Callers
	// Fail turns an error from a tool's run into what the caller reads: the fix for the caller's
	// mistake as is, anything else as a short sentence without internals (the app logs those).
	// Nil passes the error unchanged.
	Fail func(tool string, err error) error
	// Links lists references in a result, sent after the text as resource links; nil sends none.
	Links func(out any) []string
}

// AddTool registers run as the tool t with InputSchema and t's Annotations. Each call first checks
// t's class against the caller's ceiling (Callers.Allowed), then runs. The answer's text is run's
// summary, a newline and out as JSON; out is also its structured content. An error from run goes
// through tools.Fail and comes back as a tool error (Fail), which the agent can act on.
func AddTool[In, Out any](tools Tools, t Tool, description string, enums map[string][]any,
	run func(ctx context.Context, req *mcp.CallToolRequest, in In) (string, Out, error)) {
	tool := &mcp.Tool{Name: t.Name, Description: description, InputSchema: InputSchema[In](t.Name, enums), Annotations: t.Annotations()}
	mcp.AddTool(tools.Server, tool, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		if err := tools.Callers.Allowed(req, t.Class); err != nil {
			return Fail[Out](err)
		}
		summary, out, err := run(ctx, req, in)
		if err != nil {
			if tools.Fail != nil {
				err = tools.Fail(t.Name, err)
			}
			return Fail[Out](err)
		}
		data, err := json.Marshal(out)
		if err != nil {
			return Fail[Out](err)
		}
		content := []mcp.Content{&mcp.TextContent{Text: summary + "\n" + string(data)}}
		if tools.Links != nil {
			for _, ref := range tools.Links(out) {
				content = append(content, &mcp.ResourceLink{URI: ref, Name: ref})
			}
		}
		return &mcp.CallToolResult{Content: content}, out, nil
	})
}

// Handler serves an app's /mcp: MCP's Streamable HTTP transport with JSON answers and sessions
// that end after idle without a request, behind GuardRemote's two doors, and every answer marked
// Cache-Control: no-store. server returns the server for a new session; the guard has made the
// caller the token's user (auth.TokenInfoFromContext), so an app can give a caller its own server.
func Handler(keys, remote map[string]string, callers Callers, idle time.Duration, server func(*http.Request) *mcp.Server) http.Handler {
	transport := mcp.NewStreamableHTTPHandler(server, &mcp.StreamableHTTPOptions{SessionTimeout: idle, JSONResponse: true})
	guarded := GuardRemote(keys, remote, callers, transport)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		guarded.ServeHTTP(w, r)
	})
}
