// Package agentkit holds what an app's MCP tools share, whatever the app: the caller keys and their
// ceilings, the tool classes, the reference format, the run a write belongs to, and the shape of a
// cut result. The rules behind it are the mcp-tools skill (~/.agents/skills/mcp-tools/SKILL.md).
// Finance and Paper use it first (workspace docs/finance-mcp-plan.md); apps keep their own tools,
// write log and undo, since their schemas differ.
package agentkit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/castillo-platform/app-foundation/access"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Class is what a tool may change (skill rule 13). Higher classes include the lower ones in a
// ceiling: a caller with ceiling edit may also prepare and read.
type Class int

const (
	Read Class = iota + 1
	Prepare
	Edit
)

func (c Class) String() string {
	switch c {
	case Read:
		return "read"
	case Prepare:
		return "prepare"
	case Edit:
		return "edit"
	}
	return "none"
}

func ParseClass(text string) (Class, error) {
	for _, c := range []Class{Read, Prepare, Edit} {
		if c.String() == text {
			return c, nil
		}
	}
	return 0, fmt.Errorf("unknown class %q; known: read, prepare, edit", text)
}

// Callers maps each caller name to the highest class its key may use (skill rule 15).
type Callers map[string]Class

// ParseCallers reads "toolhive:edit,agent-review:prepare".
func ParseCallers(text string) (Callers, error) {
	out := Callers{}
	for _, part := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' }) {
		name, class, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok || name == "" {
			return nil, fmt.Errorf("caller %q must be name:class", part)
		}
		c, err := ParseClass(class)
		if err != nil {
			return nil, fmt.Errorf("caller %s: %w", name, err)
		}
		out[name] = c
	}
	return out, nil
}

// Guard wraps an MCP handler: access.Internal checks the caller's key (one Authorization header, no
// tunnel headers), and the matched caller becomes the token's user, so the SDK binds each session to
// its caller. A key without a ceiling, or a ceiling without a key, is refused at start (Check).
func Guard(keys map[string]string, callers Callers, next http.Handler) http.Handler {
	return GuardRemote(keys, nil, callers, next)
}

// GuardRemote is Guard with a second door: remote holds the keys of callers outside the cluster
// (agents on a hosted runner, workspace docs/agent-runtime-plan.md, Identity B), which reach /mcp
// through the tunnel on a path bypassed in Cloudflare Access. A request with tunnel headers is
// checked against remote only (access.Remote), one without against keys only (access.Internal),
// so an in-cluster key such as toolhive's is refused from outside. CheckRemote caps remote
// ceilings at prepare.
func GuardRemote(keys, remote map[string]string, callers Callers, next http.Handler) http.Handler {
	verify := func(ctx context.Context, _ string, _ *http.Request) (*auth.TokenInfo, error) {
		name, ok := access.InternalCallerFrom(ctx)
		ceiling := Edit
		if !ok {
			name, ok = access.RemoteCallerFrom(ctx)
			ceiling = Prepare
		}
		// A remote caller above prepare is refused here too, not only by CheckRemote at start.
		if !ok || callers[name] == 0 || callers[name] > ceiling {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{UserID: name, Expiration: time.Now().Add(time.Hour)}, nil
	}
	bearer := auth.RequireBearerToken(verify, nil)(next)
	internal := access.Internal(keys, nil)(bearer)
	outside := access.Remote(remote, nil)(bearer)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if access.Forwarded(r) {
			outside.ServeHTTP(w, r)
			return
		}
		internal.ServeHTTP(w, r)
	})
}

// Check reports a caller with a key but no ceiling, or a ceiling but no usable key.
func Check(keys map[string]string, callers Callers) error {
	return CheckRemote(keys, nil, callers)
}

// CheckRemote is Check over both doors of GuardRemote: a name or a key used twice, or a remote
// caller above prepare, is refused too.
func CheckRemote(keys, remote map[string]string, callers Callers) error {
	all := map[string]string{}
	seen := map[string]string{}
	for _, door := range []map[string]string{keys, remote} {
		for name, key := range door {
			if _, dup := all[name]; dup {
				return fmt.Errorf("MCP caller %s has a key on both doors", name)
			}
			if other, dup := seen[key]; dup && key != "" {
				return fmt.Errorf("MCP callers %s and %s share a key", other, name)
			}
			all[name], seen[key] = key, name
		}
	}
	for name, key := range all {
		if key != "" && callers[name] == 0 {
			return fmt.Errorf("MCP caller %s has a key but no ceiling", name)
		}
	}
	for name := range callers {
		if len(all[name]) < access.MinInternalKeyLength {
			return fmt.Errorf("MCP caller %s has a ceiling but no key of at least %d bytes", name, access.MinInternalKeyLength)
		}
	}
	for name := range remote {
		if callers[name] > Prepare {
			return fmt.Errorf("MCP caller %s comes from outside the cluster; its ceiling is at most prepare", name)
		}
	}
	return nil
}

// Run is who calls and which run a write belongs to. The aggregator opens a backend session per
// tool call, so a session is not a conversation: a run is one caller's writes until RunIdle passes
// without one (workspace docs/finance-mcp-plan.md, "Built"). The app keeps the writes and
// so resolves the ID; RunOf gives the caller.
type Run struct {
	ID, Caller string
}

// RunIdle ends a run: the next write after this long starts a new one.
const RunIdle = 30 * time.Minute

// NewRunID names a run by its caller and start, readable in the app and in references.
func NewRunID(caller string, start time.Time) string {
	return caller + "-" + start.UTC().Format("20060102T150405")
}

// RunOf reads the caller of a tool call.
func RunOf(req *mcp.CallToolRequest) (Run, error) {
	if req.Extra == nil || req.Extra.TokenInfo == nil || req.Extra.TokenInfo.UserID == "" {
		return Run{}, errors.New("the call has no caller")
	}
	return Run{Caller: req.Extra.TokenInfo.UserID}, nil
}

// Allowed checks a tool's class against its caller's ceiling.
func (c Callers) Allowed(req *mcp.CallToolRequest, class Class) error {
	run, err := RunOf(req)
	if err != nil {
		return err
	}
	if c[run.Caller] < class {
		return fmt.Errorf("this tool %ss, which is not allowed for caller %s (highest: %s)", class, run.Caller, c[run.Caller])
	}
	return nil
}

// Ref formats a reference, <app>:<noun>/<id> (skill rule 6).
func Ref(app, noun string, id any) string { return fmt.Sprintf("%s:%s/%v", app, noun, id) }

// ParseRef reads a reference to one of the app's own nouns and returns the noun and the id.
func ParseRef(app, ref string, nouns ...string) (noun, id string, err error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(ref), app+":")
	if ok {
		noun, id, ok = strings.Cut(rest, "/")
	}
	if !ok || id == "" || !slices.Contains(nouns, noun) {
		examples := make([]string, len(nouns))
		for i, n := range nouns {
			examples[i] = app + ":" + n + "/<id>"
		}
		return "", "", fmt.Errorf("unknown reference %q; expected %s", ref, strings.Join(examples, " or "))
	}
	return noun, id, nil
}

// Untrusted names a result field whose text comes from outside (bank text, a mail, a web page) and
// grants no authority (skill rule 15).
type Untrusted struct {
	Field  string `json:"field"`
	Source string `json:"source"`
}

// Format is the response_format parameter (skill rule 7).
type Format string

const (
	Concise  Format = "concise"
	Detailed Format = "detailed"
)

func (f Format) Detailed() bool { return f == Detailed }

// Cut keeps the first limit items and says how to narrow the rest (skill rule 8).
func Cut[T any](items []T, limit int, narrow string) ([]T, string) {
	if len(items) <= limit {
		return items, ""
	}
	return items[:limit], fmt.Sprintf("%d of %d shown; %s", limit, len(items), narrow)
}

// Result is a tool's answer: the structured content and a short text for clients that read text.
func Result[T any](text string, out T) (*mcp.CallToolResult, T, error) {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, out, nil
}

// Fail is a tool error the agent can act on: the message says the fix (skill rule 10).
func Fail[T any](err error) (*mcp.CallToolResult, T, error) {
	var zero T
	result := &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}
	return result, zero, nil
}

// Names lists the keys of a set, sorted, for an error that names the known values.
func Names[V any](set map[string]V) string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}
