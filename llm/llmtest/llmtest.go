// Package llmtest is a fake LLM gateway for tests: it records each chat completion request and
// answers with the reply the test chooses.
package llmtest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Request is one recorded call.
type Request struct {
	Header http.Header
	// Body is the decoded JSON body, for assertions such as Body["reasoning_effort"].
	Body map[string]any
	Raw  []byte
}

// Reply is the fake answer. The zero value is a 200 with empty content and finish reason "stop".
type Reply struct {
	Status       int
	Content      string
	Reasoning    string
	FinishReason string
	Usage        *[3]int // prompt, completion and total tokens
	// Raw, when set, is written as the body verbatim (with Status, default 200).
	Raw string
	// Stream, when set, is sent as server-sent events, one "data:" line per element; include
	// "[DONE]" to end the stream normally.
	Stream []string
}

// Server is the fake gateway. URL is its API root (ending in /v1), for llm.Config.BaseURL.
type Server struct {
	URL string

	mu       sync.Mutex
	requests []Request
}

// NewServer starts a fake gateway that answers POST /v1/chat/completions with reply.
func NewServer(t testing.TB, reply func(Request) Reply) *Server {
	t.Helper()
	s := &Server{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("llmtest: unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		req := Request{Header: r.Header.Clone(), Raw: raw}
		if err := json.Unmarshal(raw, &req.Body); err != nil {
			t.Errorf("llmtest: request body is not JSON: %v", err)
		}
		s.mu.Lock()
		s.requests = append(s.requests, req)
		s.mu.Unlock()
		answer := reply(req)
		status := answer.Status
		if status == 0 {
			status = http.StatusOK
		}
		if answer.Stream != nil {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(status)
			for _, data := range answer.Stream {
				io.WriteString(w, "data: "+data+"\n\n")
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if answer.Raw != "" {
			io.WriteString(w, answer.Raw)
			return
		}
		finish := answer.FinishReason
		if finish == "" {
			finish = "stop"
		}
		message := map[string]any{"role": "assistant", "content": answer.Content}
		if answer.Reasoning != "" {
			message["reasoning_content"] = answer.Reasoning
		}
		body := map[string]any{"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}}
		if answer.Usage != nil {
			body["usage"] = map[string]int{"prompt_tokens": answer.Usage[0], "completion_tokens": answer.Usage[1], "total_tokens": answer.Usage[2]}
		}
		json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(server.Close)
	s.URL = server.URL + "/v1"
	return s
}

// Requests returns the calls received so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}
