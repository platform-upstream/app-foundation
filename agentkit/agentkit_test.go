package agentkit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const key = "0123456789abcdef0123456789abcdef"

func TestParseCallers(t *testing.T) {
	callers, err := ParseCallers("toolhive:edit, agent-review:prepare")
	if err != nil || callers["toolhive"] != Edit || callers["agent-review"] != Prepare {
		t.Fatalf("ParseCallers = %v, %v", callers, err)
	}
	for _, bad := range []string{"toolhive", "toolhive:write", ":read"} {
		if _, err := ParseCallers(bad); err == nil {
			t.Errorf("ParseCallers(%q) accepted", bad)
		}
	}
}

func TestCheck(t *testing.T) {
	if err := Check(map[string]string{"toolhive": key}, Callers{"toolhive": Read}); err != nil {
		t.Fatal(err)
	}
	if err := Check(map[string]string{"toolhive": key}, Callers{}); err == nil {
		t.Error("a key without a ceiling passed")
	}
	if err := Check(map[string]string{"toolhive": "short"}, Callers{"toolhive": Read}); err == nil {
		t.Error("a ceiling with a short key passed")
	}
}

func TestGuard(t *testing.T) {
	var caller string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller = auth.TokenInfoFromContext(r.Context()).UserID
	})
	h := Guard(map[string]string{"toolhive": key}, Callers{"toolhive": Read}, next)
	for _, tc := range []struct {
		header string
		status int
	}{{"Bearer " + key, 200}, {"Bearer " + strings.Repeat("x", 32), 401}, {"", 401}} {
		caller = ""
		r := httptest.NewRequest("POST", "/mcp", nil)
		if tc.header != "" {
			r.Header.Set("Authorization", tc.header)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Errorf("%q: status %d, want %d", tc.header, w.Code, tc.status)
		}
		if tc.status == 200 && caller != "toolhive" {
			t.Errorf("caller %q, want toolhive", caller)
		}
	}
}

func TestAllowed(t *testing.T) {
	req := &mcp.CallToolRequest{Extra: &mcp.RequestExtra{TokenInfo: &auth.TokenInfo{UserID: "toolhive"}}}
	callers := Callers{"toolhive": Prepare}
	if err := callers.Allowed(req, Prepare); err != nil {
		t.Fatal(err)
	}
	if err := callers.Allowed(req, Edit); err == nil || !strings.Contains(err.Error(), "highest: prepare") {
		t.Errorf("edit above a prepare ceiling: %v", err)
	}
	if err := callers.Allowed(&mcp.CallToolRequest{}, Read); err == nil {
		t.Error("a call without a caller passed")
	}
}

func TestRefs(t *testing.T) {
	ref := Ref("memory", "note", "food:lina:preferences")
	if ref != "memory:note/food:lina:preferences" {
		t.Fatal(ref)
	}
	noun, id, err := ParseRef("memory", ref, "note", "run")
	if err != nil || noun != "note" || id != "food:lina:preferences" {
		t.Fatalf("ParseRef = %q %q %v", noun, id, err)
	}
	if _, _, err := ParseRef("memory", "finance:booking/1", "note"); err == nil || !strings.Contains(err.Error(), "memory:note/<id>") {
		t.Errorf("a foreign reference: %v", err)
	}
}

func TestCut(t *testing.T) {
	items, note := Cut([]int{1, 2, 3}, 2, "filter by person")
	if len(items) != 2 || note != "2 of 3 shown; filter by person" {
		t.Fatalf("Cut = %v %q", items, note)
	}
	if _, note := Cut([]int{1}, 2, "x"); note != "" {
		t.Errorf("an uncut result says %q", note)
	}
}

func TestNewRunID(t *testing.T) {
	if id := NewRunID("toolhive", mustTime(t)); id != "toolhive-20261028T101500" {
		t.Fatal(id)
	}
}

func mustTime(t *testing.T) time.Time {
	at, err := time.Parse(time.RFC3339, "2026-10-28T10:15:00Z")
	if err != nil {
		t.Fatal(err)
	}
	return at
}

func TestCheckRemote(t *testing.T) {
	agent := strings.Repeat("a", 32)
	if err := CheckRemote(map[string]string{"toolhive": key}, map[string]string{"agent-x": agent}, Callers{"toolhive": Edit, "agent-x": Prepare}); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		keys, remote map[string]string
		callers      Callers
	}{
		"remote above prepare":   {nil, map[string]string{"agent-x": agent}, Callers{"agent-x": Edit}},
		"shared key":             {map[string]string{"toolhive": key}, map[string]string{"agent-x": key}, Callers{"toolhive": Edit, "agent-x": Read}},
		"name on both doors":     {map[string]string{"agent-x": key}, map[string]string{"agent-x": agent}, Callers{"agent-x": Read}},
		"remote without ceiling": {nil, map[string]string{"agent-x": agent}, Callers{}},
	} {
		if err := CheckRemote(tc.keys, tc.remote, tc.callers); err == nil {
			t.Errorf("%s passed", name)
		}
	}
}

func TestGuardRemote(t *testing.T) {
	agent := strings.Repeat("a", 32)
	var caller string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller = auth.TokenInfoFromContext(r.Context()).UserID
	})
	h := GuardRemote(map[string]string{"toolhive": key}, map[string]string{"agent-x": agent}, Callers{"toolhive": Edit, "agent-x": Prepare}, next)
	for _, tc := range []struct {
		key       string
		forwarded bool
		status    int
		caller    string
	}{
		{key, false, 200, "toolhive"},
		{agent, true, 200, "agent-x"},
		// Each key opens only its own door: toolhive's is refused from outside, the agent's inside.
		{key, true, 401, ""},
		{agent, false, 401, ""},
		{strings.Repeat("x", 32), true, 401, ""},
	} {
		caller = ""
		r := httptest.NewRequest("POST", "/mcp", nil)
		r.Header.Set("Authorization", "Bearer "+tc.key)
		if tc.forwarded {
			r.Header.Set("Cf-Ray", "8f00")
			r.Header.Set("X-Forwarded-For", "203.0.113.9")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status || caller != tc.caller {
			t.Errorf("key %.4s forwarded %v: status %d caller %q, want %d %q", tc.key, tc.forwarded, w.Code, caller, tc.status, tc.caller)
		}
	}
}

func TestGuardRemoteRefusesARemoteCallerAbovePrepare(t *testing.T) {
	agent := strings.Repeat("a", 32)
	h := GuardRemote(nil, map[string]string{"agent-x": agent}, Callers{"agent-x": Edit}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := httptest.NewRequest("POST", "/mcp", nil)
	r.Header.Set("Authorization", "Bearer "+agent)
	r.Header.Set("Cf-Ray", "8f00")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("status %d, want 401", w.Code)
	}
}
