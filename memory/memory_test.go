package memory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	foundationsqlite "github.com/castillo-platform/app-foundation/sqlite"
)

func TestNotesCachesFallsBackAndOverlaysPending(t *testing.T) {
	var status atomic.Int64
	status.Store(http.StatusOK)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(int(status.Load()))
		if status.Load() == http.StatusOK {
			_ = json.NewEncoder(w).Encode(map[string]any{"notes": []Note{{ID: "food/nik/taste", Person: "nik", Domain: "food", Key: "taste", Revision: 1}}})
		}
	}))
	defer server.Close()
	client := newTestClient(t, Config{BaseURL: server.URL, APIKey: "secret", App: "cook"})

	notes, err := client.Notes(context.Background(), "food")
	if err != nil || len(notes) != 1 || notes[0].Revision != 1 {
		t.Fatalf("initial notes = %#v, error = %v", notes, err)
	}
	if err := client.QueueNote(Note{ID: "food/nik/taste", Person: "nik", Domain: "food", Key: "taste", Revision: 2, Markdown: "pending"}); err != nil {
		t.Fatal(err)
	}
	status.Store(http.StatusServiceUnavailable)
	notes, err = client.Notes(context.Background(), "food")
	if err != nil || len(notes) != 1 || notes[0].Revision != 2 || notes[0].Owner != "cook" || len(notes[0].Visibility) != 1 || notes[0].Visibility[0] != "cook" {
		t.Fatalf("fallback notes = %#v, error = %v", notes, err)
	}
	status.Store(http.StatusUnauthorized)
	if _, err := client.Notes(context.Background(), "food"); err == nil {
		t.Fatal("authorization failure used cached private data")
	}
}

func TestFlushPersistsAcceptedWriteAndClearsOutbox(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.EscapedPath() != "/v1/notes/food%2Fnik%2Ftaste" {
			t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
		}
		var note Note
		if err := json.NewDecoder(r.Body).Decode(&note); err != nil {
			t.Error(err)
		}
		if note.Owner != "cook" || len(note.Visibility) != 1 || note.Visibility[0] != "cook" {
			t.Errorf("queued ownership = %#v", note)
		}
		note.Revision++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(note)
	}))
	defer server.Close()
	client := newTestClient(t, Config{BaseURL: server.URL, APIKey: "secret", App: "cook"})
	if err := client.QueueNote(Note{ID: "food/nik/taste", Person: "nik", Domain: "food", Key: "taste"}); err != nil {
		t.Fatal(err)
	}
	if err := client.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := client.Status()
	if !got.Configured || got.Pending != 0 || got.Conflicts != 0 || got.LastSync == "" {
		t.Fatalf("status = %#v", got)
	}
}

func TestFlushMarksRealConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusConflict)
			return
		}
		_ = json.NewEncoder(w).Encode(Note{ID: "food/nik/taste", Person: "nik", Domain: "food", Key: "taste", Markdown: "remote"})
	}))
	defer server.Close()
	client := newTestClient(t, Config{BaseURL: server.URL, APIKey: "secret", App: "cook"})
	if err := client.QueueNote(Note{ID: "food/nik/taste", Person: "nik", Domain: "food", Key: "taste", Markdown: "local"}); err != nil {
		t.Fatal(err)
	}
	if err := client.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := client.Status(); got.Pending != 1 || got.Conflicts != 1 {
		t.Fatalf("status = %#v", got)
	}
	if err := client.DiscardConflicts(); err != nil {
		t.Fatal(err)
	}
	if got := client.Status(); got.Pending != 0 || got.Conflicts != 0 {
		t.Fatalf("status after discard = %#v", got)
	}
}

func TestClientBoundsTransportAndDisabledBehavior(t *testing.T) {
	db, err := foundationsqlite.Open(context.Background(), t.TempDir()+"/memory.sqlite", foundationsqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	disabled, err := New(db, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Enabled() || !errors.Is(disabled.QueueNote(Note{}), ErrNotConfigured) || disabled.Flush(context.Background()) != nil {
		t.Fatal("disabled client behavior changed")
	}
	provided := &http.Client{Timeout: time.Minute}
	configured, err := New(db, Config{BaseURL: "https://memory.invalid", APIKey: "secret", App: "watch", HTTPClient: provided})
	if err != nil {
		t.Fatal(err)
	}
	if configured.http.Timeout != DefaultTimeout || provided.Timeout != time.Minute {
		t.Fatalf("timeouts = %s/%s", configured.http.Timeout, provided.Timeout)
	}
}

func TestResponseLimitAndHelpers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"people":[]}` + strings.Repeat(" ", int(MaxResponseBytes))))
	}))
	defer server.Close()
	client := newTestClient(t, Config{BaseURL: server.URL, APIKey: "secret", App: "watch"})
	if _, err := client.People(context.Background()); err == nil || err.Error() != "memory_response_too_large" {
		t.Fatalf("People error = %v", err)
	}
	note, ok := Find([]Note{{Person: "nik", Key: "taste"}}, "nik", "taste")
	if !ok || note.Key != "taste" {
		t.Fatalf("Find = %#v, %v", note, ok)
	}
	lines := make([]string, 25)
	for i := range lines {
		lines[i] = "line"
	}
	if got := Summary("header\n", lines); strings.Count(got, "line") != 20 {
		t.Fatalf("Summary retained wrong line count: %q", got)
	}
}

func newTestClient(t *testing.T, config Config) *Client {
	t.Helper()
	db, err := foundationsqlite.Open(context.Background(), t.TempDir()+"/app.sqlite", foundationsqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	client, err := New(db, config)
	if err != nil {
		t.Fatal(err)
	}
	return client
}
