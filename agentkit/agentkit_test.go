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
